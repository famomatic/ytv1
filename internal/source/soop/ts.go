package soop

// Minimal MPEG-TS muxer: wraps the agent's H.264 Annex-B access units and ADTS
// AAC frames into a single transport stream, emitting each 188-byte packet as it
// is produced. Unlike an ffmpeg fragmented-MP4/HLS remux (which buffers a whole
// fragment before flushing), this writes incrementally, so a live HTTP download
// or player receives data immediately. Presentation timestamps are synthesized
// (video at the constant frame rate, audio from the AAC sample count) which is
// enough for smooth playback; players re-time off the PTS/PCR.
//
// Single-program, single-goroutine (driven from the deframe callback). Validated
// to produce a decodable H.264 1920x1080 + AAC 48k stream (§14.25).

import "io"

const (
	tsLen   = 188
	tsSync  = 0x47
	tsPMT   = 0x1000
	tsVideo = 0x0100
	tsAudio = 0x0101
	sidVid  = 0xE0
	sidAud  = 0xC0
	tsPTSHz = 90000
)

type tsMuxer struct {
	w                          io.Writer
	ccPAT, ccPMT, ccVid, ccAud byte
	vpts, apts                 uint64
	pktsSinceSI                int
	err                        error
}

func newTSMuxer(w io.Writer) *tsMuxer { return &tsMuxer{w: w, vpts: 10000, apts: 10000} }

func (m *tsMuxer) write(b []byte) {
	if m.err != nil {
		return
	}
	_, m.err = m.w.Write(b)
}

// writeVideo muxes one H.264 Annex-B access unit.
func (m *tsMuxer) writeVideo(au []byte) error {
	m.emitTables()
	m.writePES(tsVideo, &m.ccVid, sidVid, m.vpts, true, au)
	m.vpts += tsPTSHz / 60
	return m.err
}

// writeAudio muxes a chunk of concatenated ADTS AAC frames, one PES per frame so
// each carries its own PTS.
func (m *tsMuxer) writeAudio(chunk []byte) error {
	for _, fr := range splitADTS(chunk) {
		m.writePES(tsAudio, &m.ccAud, sidAud, m.apts, false, fr)
		m.apts += tsPTSHz * 1024 / 48000
	}
	return m.err
}

func (m *tsMuxer) emitTables() {
	// (Re)emit PAT/PMT at the start and periodically so a mid-stream reader can
	// sync.
	if m.pktsSinceSI == 0 || m.pktsSinceSI > 40 {
		m.writeSection(0x0000, &m.ccPAT, buildTSPAT())
		m.writeSection(tsPMT, &m.ccPMT, buildTSPMT())
		m.pktsSinceSI = 0
	}
	m.pktsSinceSI++
}

func (m *tsMuxer) writeSection(pid uint16, cc *byte, sec []byte) {
	pkt := make([]byte, 0, tsLen)
	pkt = append(pkt, tsSync, byte(0x40|(pid>>8)), byte(pid&0xFF), byte(0x10|(*cc&0x0F)))
	*cc = (*cc + 1) & 0x0F
	pkt = append(pkt, 0x00) // pointer_field
	pkt = append(pkt, sec...)
	for len(pkt) < tsLen {
		pkt = append(pkt, 0xFF)
	}
	m.write(pkt[:tsLen])
}

func (m *tsMuxer) writePES(pid uint16, cc *byte, streamID byte, pts uint64, withPCR bool, au []byte) {
	pes := append(tsPESHeader(streamID, pts, len(au)), au...)
	first := true
	for len(pes) > 0 {
		pkt := make([]byte, 4, tsLen)
		pkt[0] = tsSync
		pkt[1] = byte(pid >> 8)
		if first {
			pkt[1] |= 0x40 // payload_unit_start
		}
		pkt[2] = byte(pid & 0xFF)
		var af []byte
		if first && withPCR {
			af = []byte{0x07, 0x10,
				byte(pts >> 25), byte(pts >> 17), byte(pts >> 9), byte(pts >> 1),
				byte((pts&1)<<7) | 0x7E, 0x00}
		}
		avail := tsLen - 4 - len(af)
		if len(pes) < avail {
			need := avail - len(pes)
			if len(af) == 0 {
				if need == 1 {
					af = []byte{0x00}
				} else {
					af = make([]byte, need)
					af[0] = byte(need - 1)
					af[1] = 0x00
					for i := 2; i < need; i++ {
						af[i] = 0xFF
					}
				}
			} else {
				old := len(af)
				af = append(af, make([]byte, need)...)
				for i := old; i < len(af); i++ {
					af[i] = 0xFF
				}
				af[0] = byte(len(af) - 1)
			}
			avail = tsLen - 4 - len(af)
		}
		if len(af) > 0 {
			pkt[3] = 0x30 | (*cc & 0x0F) // adaptation + payload
		} else {
			pkt[3] = 0x10 | (*cc & 0x0F) // payload only
		}
		*cc = (*cc + 1) & 0x0F
		pkt = append(pkt, af...)
		take := min(avail, len(pes))
		pkt = append(pkt, pes[:take]...)
		pes = pes[take:]
		for len(pkt) < tsLen {
			pkt = append(pkt, 0xFF)
		}
		m.write(pkt[:tsLen])
		first = false
	}
}

func tsPESHeader(streamID byte, pts uint64, payloadLen int) []byte {
	h := []byte{0x00, 0x00, 0x01, streamID}
	opt := []byte{0x80, 0x80, 0x05,
		byte(0x21 | ((pts >> 29) & 0x0E)),
		byte(pts >> 22), byte(0x01 | ((pts >> 14) & 0xFE)),
		byte(pts >> 7), byte(0x01 | ((pts << 1) & 0xFE))}
	total := len(opt) + payloadLen
	length := 0 // 0 = unbounded (allowed; simplest for large video AUs)
	if total <= 0xFFFF {
		length = total
	}
	h = append(h, byte(length>>8), byte(length))
	return append(h, opt...)
}

func buildTSPAT() []byte {
	sec := []byte{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00,
		0x00, 0x01, byte(0xE0 | (tsPMT >> 8)), byte(tsPMT & 0xFF)}
	return appendTSCRC(sec)
}

func buildTSPMT() []byte {
	sec := []byte{0x02, 0xB0, 0x17, 0x00, 0x01, 0xC1, 0x00, 0x00,
		byte(0xE0 | (tsVideo >> 8)), byte(tsVideo & 0xFF), 0xF0, 0x00,
		0x1B, byte(0xE0 | (tsVideo >> 8)), byte(tsVideo & 0xFF), 0xF0, 0x00,
		0x0F, byte(0xE0 | (tsAudio >> 8)), byte(tsAudio & 0xFF), 0xF0, 0x00}
	return appendTSCRC(sec)
}

func appendTSCRC(sec []byte) []byte {
	c := tsCRC32(sec)
	return append(sec, byte(c>>24), byte(c>>16), byte(c>>8), byte(c))
}

func tsCRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, x := range data {
		crc ^= uint32(x) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// splitADTS splits concatenated ADTS AAC frames into individual frames.
func splitADTS(b []byte) [][]byte {
	var out [][]byte
	for i := 0; i+7 <= len(b); {
		if b[i] != 0xFF || b[i+1]&0xF0 != 0xF0 {
			i++
			continue
		}
		l := (int(b[i+3]&0x03) << 11) | (int(b[i+4]) << 3) | (int(b[i+5]) >> 5)
		if l < 7 || i+l > len(b) {
			break
		}
		out = append(out, b[i:i+l])
		i += l
	}
	return out
}
