package soop

// Remux for the agent stream. The deframed elementary streams (H.264 Annex-B
// video, ADTS AAC audio) are captured to two temp files, then muxed once with
// ffmpeg into a fragmented MP4 (video PTS at the constant frame rate, audio from
// its sample count — the combination that reliably reconstructs a correct file;
// see §14.25). ffmpeg owns framing/PTS. A live dual-feed mux is timing-finicky,
// so capture-then-mux is used for correctness; the muxed output is written to out
// when the stream ends (or the context is cancelled).

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ffmpegBinary resolves the ffmpeg executable (YTV1_FFMPEG override, else PATH).
func ffmpegBinary() string {
	if p := os.Getenv("YTV1_FFMPEG"); p != "" {
		return p
	}
	return "ffmpeg"
}

// esMuxer captures the two elementary streams to temp files and muxes them into
// out on close.
type esMuxer struct {
	video *os.File
	audio *os.File
	out   io.Writer
}

func startESMux(out io.Writer) (*esMuxer, error) {
	v, err := os.CreateTemp("", "ytv1-soop-*.h264")
	if err != nil {
		return nil, err
	}
	a, err := os.CreateTemp("", "ytv1-soop-*.aac")
	if err != nil {
		v.Close()
		os.Remove(v.Name())
		return nil, err
	}
	return &esMuxer{video: v, audio: a, out: out}, nil
}

func (m *esMuxer) writeVideo(b []byte) error {
	_, err := m.video.Write(b)
	return err
}

func (m *esMuxer) writeAudio(b []byte) error {
	_, err := m.audio.Write(b)
	return err
}

// close finalizes the temp files, muxes them to out via ffmpeg, and cleans up.
func (m *esMuxer) close() {
	vName, aName := m.video.Name(), m.audio.Name()
	m.video.Close()
	m.audio.Close()
	defer os.Remove(vName)
	defer os.Remove(aName)

	fi, _ := os.Stat(vName)
	if fi == nil || fi.Size() == 0 {
		return
	}
	// Fragmented MP4: streamable, and tolerant of the raw ES's minimal timing.
	cmd := exec.Command(ffmpegBinary(),
		"-hide_banner", "-loglevel", "error",
		"-r", "60", "-i", vName,
		"-i", aName,
		"-c", "copy", "-bsf:a", "aac_adtstoasc",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4", "pipe:1",
	)
	cmd.Stdout = m.out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "soop: ffmpeg remux: %v\n", err)
	}
}
