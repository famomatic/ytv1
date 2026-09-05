package timeshift

// Minimal MPEG-TS parsing helpers for segmentation: locate a packet payload,
// read the PMT PID from the PAT, the video PID from the PMT, and detect a video
// keyframe access unit plus its PTS from a PES header. Only the fields needed to
// cut keyframe-aligned segments are decoded.

// payloadOf returns the payload bytes of a TS packet, skipping the adaptation
// field. When skipPointer is set (PSI packets — PAT/PMT — carry a pointer_field
// before the section), the pointer is skipped too so the section starts at index
// 0. PES packets pass skipPointer=false. Returns nil if there is no payload.
func payloadOf(pkt []byte, skipPointer bool) []byte {
	if len(pkt) < tsPacketLen {
		return nil
	}
	afc := (pkt[3] >> 4) & 0x3
	off := 4
	if afc == 2 || afc == 3 { // adaptation field present
		if len(pkt) < 5 {
			return nil
		}
		off += 1 + int(pkt[4])
	}
	if afc == 2 || off >= tsPacketLen { // adaptation only, or overrun → no payload
		return nil
	}
	payload := pkt[off:]
	if skipPointer {
		if len(payload) < 1 {
			return nil
		}
		ptr := int(payload[0])
		if 1+ptr > len(payload) {
			return nil
		}
		payload = payload[1+ptr:]
	}
	return payload
}

// parsePATFirstPMTPID returns the PMT PID of the first program (program_number
// != 0) in a PAT section payload, or -1.
func parsePATFirstPMTPID(sec []byte) int {
	// section: table_id(1) sect_synt+len(2) tsid(2) ver/cur(1) sect#(1) last#(1)
	// then N * [program_number(2) reserved+PID(2)], then CRC(4).
	if len(sec) < 12 || sec[0] != 0x00 {
		return -1
	}
	sectionLen := (int(sec[1]&0x0F) << 8) | int(sec[2])
	end := 3 + sectionLen - 4 // exclude CRC
	if end > len(sec) {
		end = len(sec)
	}
	for i := 8; i+4 <= end; i += 4 {
		prog := (int(sec[i]) << 8) | int(sec[i+1])
		pid := (int(sec[i+2]&0x1F) << 8) | int(sec[i+3])
		if prog != 0 {
			return pid
		}
	}
	return -1
}

// parsePMTVideoPID returns the elementary PID of the first video stream (H.264
// stream_type 0x1B or HEVC 0x24) in a PMT section payload, or -1.
func parsePMTVideoPID(sec []byte) int { pid, _ := parsePMTVideo(sec); return pid }
func parsePMTVideo(sec []byte) (int, byte) {
	if len(sec) < 12 || sec[0] != 0x02 {
		return -1, 0
	}
	sectionLen := (int(sec[1]&0x0F) << 8) | int(sec[2])
	end := 3 + sectionLen - 4
	if end > len(sec) {
		end = len(sec)
	}
	progInfoLen := (int(sec[10]&0x0F) << 8) | int(sec[11])
	i := 12 + progInfoLen
	for i+5 <= end {
		streamType := sec[i]
		pid := (int(sec[i+1]&0x1F) << 8) | int(sec[i+2])
		esInfoLen := (int(sec[i+3]&0x0F) << 8) | int(sec[i+4])
		if streamType == 0x1B || streamType == 0x24 {
			return pid, streamType
		}
		i += 5 + esInfoLen
	}
	return -1, 0
}

// isKeyframePES detects an H.264 IDR in a reassembled PES access unit.
func isKeyframePES(pes []byte) bool {
	return isKeyframeCodec(pes, 0x1b)
}
func isKeyframeCodec(pes []byte, codec byte) bool {
	es := pesElementaryData(pes)
	// Annex-B: scan for start codes (00 00 01) and inspect the NAL type.
	for i := 0; i+3 < len(es); i++ {
		if es[i] == 0 && es[i+1] == 0 && es[i+2] == 1 {
			if codec == 0x24 {
				typ := (es[i+3] >> 1) & 0x3f
				if typ >= 16 && typ <= 21 {
					return true
				}
				if typ <= 31 {
					return false
				}
				continue
			}
			nalType := es[i+3] & 0x1F
			if nalType == 5 {
				return true
			}
			if nalType == 1 { // a non-IDR slice: this AU is not a keyframe
				return false
			}
			i += 2
		}
	}
	return false
}

// pesElementaryData returns the elementary-stream bytes after the PES header.
func pesElementaryData(pes []byte) []byte {
	if len(pes) < 9 || pes[0] != 0 || pes[1] != 0 || pes[2] != 1 {
		return nil
	}
	headerDataLen := int(pes[8])
	start := 9 + headerDataLen
	if start > len(pes) {
		return nil
	}
	return pes[start:]
}

// pesPTS extracts the 90 kHz PTS from a PES header, if present.
func pesPTS(pes []byte) (int64, bool) {
	if len(pes) < 14 || pes[0] != 0 || pes[1] != 0 || pes[2] != 1 {
		return 0, false
	}
	if pes[7]&0x80 == 0 { // PTS_DTS_flags: no PTS
		return 0, false
	}
	p := pes[9:14]
	pts := (int64(p[0]&0x0E) << 29) |
		(int64(p[1]) << 22) |
		(int64(p[2]&0xFE) << 14) |
		(int64(p[3]) << 7) |
		(int64(p[4]) >> 1)
	return pts, true
}
