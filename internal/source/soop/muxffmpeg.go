package soop

// Streaming remux for the agent stream. The deframed elementary streams (H.264
// Annex-B video, ADTS AAC audio) are fed to ffmpeg over two loopback TCP
// connections; ffmpeg copies them into a fragmented MP4 written incrementally to
// out (validated at ~8 Mbps 1080p). ffmpeg owns framing/PTS: -r fixes the
// constant video frame rate, the ADTS demuxer derives audio PTS. See §14.25.

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

func ffmpegBinary() string {
	if p := os.Getenv("YTV1_FFMPEG"); p != "" {
		return p
	}
	return "ffmpeg"
}

type esMuxer struct {
	cmd     *exec.Cmd
	video   net.Conn
	vListen net.Listener
	aListen net.Listener

	mu       sync.Mutex
	audio    net.Conn
	audioBuf []byte
	closed   sync.Once
}

func startESMux(out io.Writer) (*esMuxer, error) {
	vln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	aln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		vln.Close()
		return nil, err
	}
	vPort := vln.Addr().(*net.TCPAddr).Port
	aPort := aln.Addr().(*net.TCPAddr).Port

	cmd := exec.Command(ffmpegBinary(),
		"-hide_banner", "-loglevel", "error",
		"-fflags", "+genpts", "-r", "60", "-f", "h264", "-i", fmt.Sprintf("tcp://127.0.0.1:%d", vPort),
		"-f", "aac", "-i", fmt.Sprintf("tcp://127.0.0.1:%d", aPort),
		"-vsync", "passthrough", "-c", "copy", "-bsf:a", "aac_adtstoasc",
		"-max_interleave_delta", "0", "-flush_packets", "1", "-frag_duration", "500000",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4", "pipe:1",
	)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		vln.Close()
		aln.Close()
		return nil, fmt.Errorf("soop: start ffmpeg: %w", err)
	}

	m := &esMuxer{cmd: cmd, vListen: vln, aListen: aln}

	vch := make(chan net.Conn, 1)
	go func() { c, _ := vln.Accept(); vch <- c }()
	select {
	case c := <-vch:
		if c == nil {
			m.close()
			return nil, fmt.Errorf("soop: ffmpeg did not connect to video feed")
		}
		m.video = c
	case <-time.After(10 * time.Second):
		m.close()
		return nil, fmt.Errorf("soop: ffmpeg video-feed timeout")
	}

	go func() {
		c, err := aln.Accept()
		if err != nil {
			return
		}
		m.mu.Lock()
		m.audio = c
		buf := m.audioBuf
		m.audioBuf = nil
		m.mu.Unlock()
		if len(buf) > 0 {
			_, _ = c.Write(buf)
		}
	}()
	return m, nil
}

func (m *esMuxer) writeVideo(b []byte) error { _, err := m.video.Write(b); return err }

func (m *esMuxer) writeAudio(b []byte) error {
	m.mu.Lock()
	c := m.audio
	if c == nil {
		if len(m.audioBuf) < 1<<20 {
			m.audioBuf = append(m.audioBuf, b...)
		}
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	_, err := c.Write(b)
	return err
}

func (m *esMuxer) close() { m.closed.Do(m.doClose) }

func (m *esMuxer) doClose() {
	if m.video != nil {
		m.video.Close()
	}
	m.mu.Lock()
	if m.audio != nil {
		m.audio.Close()
	}
	m.mu.Unlock()
	if m.vListen != nil {
		m.vListen.Close()
	}
	if m.aListen != nil {
		m.aListen.Close()
	}
	if m.cmd != nil {
		done := make(chan struct{})
		go func() { _ = m.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = m.cmd.Process.Kill()
			<-done
		}
	}
}
