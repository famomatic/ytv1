package muxer

import (
	"bytes"
	"math"
	"os"
)

// writeMinimalWebM writes a tiny but spec-valid WebM file to path with a
// single track: a VP9 video track when video is true, else an Opus audio
// track. The file is built directly from EBML bytes so the test does not
// depend on puremux's internal packages (which are not importable outside
// the puremux module).
func writeMinimalWebM(path string, video bool) error {
	var (
		codecID   string
		trackType uint64 // 1 = video, 2 = audio
	)
	if video {
		codecID = "V_VP9"
		trackType = 1
	} else {
		codecID = "A_OPUS"
		trackType = 2
	}

	trackEntry := bytes.NewBuffer(nil)
	trackEntry.Write(ebmlUint(0xD7, 1))               // TrackNumber = 1
	trackEntry.Write(ebmlUint(0x73C5, 1))             // TrackUID = 1
	trackEntry.Write(ebmlUint(0x83, trackType))        // TrackType
	trackEntry.Write(ebmlBytes(0x86, []byte(codecID))) // CodecID
	if video {
		videoEl := bytes.NewBuffer(nil)
		videoEl.Write(ebmlUint(0xB0, 320)) // PixelWidth
		videoEl.Write(ebmlUint(0xBA, 240)) // PixelHeight
		trackEntry.Write(ebmlMaster(0xE0, videoEl.Bytes())) // Video
	} else {
		audioEl := bytes.NewBuffer(nil)
		audioEl.Write(ebmlUint(0x9F, 2))             // Channels = 2
		audioEl.Write(ebmlFloat(0xB5, 48000.0))      // SamplingFrequency
		trackEntry.Write(ebmlMaster(0xE1, audioEl.Bytes())) // Audio
	}

	tracks := ebmlMaster(0x1654AE6B, ebmlMaster(0xAE, trackEntry.Bytes()))

	info := bytes.NewBuffer(nil)
	info.Write(ebmlUint(0x2AD7B1, 1_000_000))           // TimecodeScale
	info.Write(ebmlBytes(0x4D80, []byte("ytv1-test")))  // MuxingApp
	info.Write(ebmlBytes(0x5741, []byte("ytv1-test")))  // WritingApp

	cluster := bytes.NewBuffer(nil)
	cluster.Write(ebmlUint(0xE7, 0)) // Timestamp = 0
	cluster.Write(ebmlBytes(0xA3, simpleBlock(1, 0, video, []byte{0x01, 0x02}))) // SimpleBlock

	segmentBody := bytes.NewBuffer(nil)
	segmentBody.Write(ebmlMaster(0x1549A966, info.Bytes()))    // Info
	segmentBody.Write(tracks)                                  // Tracks
	segmentBody.Write(ebmlMaster(0x1F43B675, cluster.Bytes())) // Cluster

	header := bytes.NewBuffer(nil)
	header.Write(ebmlUint(0x4286, 1))               // EBMLVersion = 1
	header.Write(ebmlUint(0x42F7, 1))               // EBMLReadVersion = 1
	header.Write(ebmlUint(0x42F2, 4))               // EBMLMaxIDLength = 4
	header.Write(ebmlUint(0x42F3, 8))               // EBMLMaxSizeLength = 8
	header.Write(ebmlBytes(0x4282, []byte("webm")))  // DocType
	header.Write(ebmlUint(0x4287, 4))               // DocTypeVersion = 4
	header.Write(ebmlUint(0x4285, 2))               // DocTypeReadVersion = 2

	out := bytes.NewBuffer(nil)
	out.Write(ebmlMaster(0x1A45DFA3, header.Bytes()))      // EBML
	out.Write(ebmlUnknownSizeSegment(segmentBody.Bytes())) // Segment

	return os.WriteFile(path, out.Bytes(), 0o644)
}

// ebmlMaster writes an EBML element: ID + size VINT + payload.
func ebmlMaster(id uint32, payload []byte) []byte {
	var b bytes.Buffer
	b.Write(ebmlID(id))
	b.Write(ebmlSize(uint64(len(payload))))
	b.Write(payload)
	return b.Bytes()
}

// ebmlUint writes an unsigned-integer element: ID + size + big-endian value.
func ebmlUint(id uint32, val uint64) []byte {
	payload := uintBytes(val)
	var b bytes.Buffer
	b.Write(ebmlID(id))
	b.Write(ebmlSize(uint64(len(payload))))
	b.Write(payload)
	return b.Bytes()
}

// ebmlBytes writes a binary/string element: ID + size + raw bytes.
func ebmlBytes(id uint32, val []byte) []byte {
	var b bytes.Buffer
	b.Write(ebmlID(id))
	b.Write(ebmlSize(uint64(len(val))))
	b.Write(val)
	return b.Bytes()
}

// ebmlFloat writes an 8-byte big-endian double element.
func ebmlFloat(id uint32, val float64) []byte {
	payload := doubleBytes(val)
	var b bytes.Buffer
	b.Write(ebmlID(id))
	b.Write(ebmlSize(uint64(len(payload))))
	b.Write(payload)
	return b.Bytes()
}

// ebmlID encodes a class-A/B/C/D element ID as 1..4 bytes MSB-first.
func ebmlID(id uint32) []byte {
	switch {
	case id <= 0xFF:
		return []byte{byte(id)}
	case id <= 0xFFFF:
		return []byte{byte(id >> 8), byte(id)}
	case id <= 0xFFFFFF:
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	default:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	}
}

// ebmlSize encodes a VINT size in the narrowest width.
func ebmlSize(val uint64) []byte {
	if val == 0 {
		return []byte{0x80}
	}
	for width := 1; width <= 8; width++ {
		if val>>vintDataBits(width) != 0 {
			continue
		}
		out := make([]byte, width)
		out[0] = byte(0x80 >> uint(width-1))
		v := val
		for i := width - 1; i >= 0; i-- {
			out[i] |= byte(v & 0xFF)
			v >>= 8
		}
		return out
	}
	return []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
}

// vintDataBits returns the data-bit count for a VINT width.
func vintDataBits(width int) uint {
	if width == 8 {
		return 56
	}
	return uint(8-width) + uint(8*(width-1))
}

// uintBytes returns the minimal big-endian encoding of val (at least 1 byte).
func uintBytes(val uint64) []byte {
	if val == 0 {
		return []byte{0}
	}
	var buf []byte
	for v := val; v > 0; v >>= 8 {
		buf = append([]byte{byte(v)}, buf...)
	}
	return buf
}

// doubleBytes returns the 8-byte big-endian IEEE 754 double encoding of val.
func doubleBytes(val float64) []byte {
	bits := math.Float64bits(val)
	return []byte{
		byte(bits >> 56), byte(bits >> 48), byte(bits >> 40), byte(bits >> 32),
		byte(bits >> 24), byte(bits >> 16), byte(bits >> 8), byte(bits),
	}
}

// simpleBlock builds a SimpleBlock payload: track VINT + int16 timecode +
// flags byte + data. Track number 1 fits in a width-1 VINT (0x81).
func simpleBlock(trackNum int, relTC int16, keyframe bool, data []byte) []byte {
	flags := byte(0)
	if keyframe {
		flags |= 0x80
	}
	out := []byte{0x80 | byte(trackNum), byte(relTC >> 8), byte(relTC), flags}
	out = append(out, data...)
	return out
}

// ebmlUnknownSizeSegment writes a Segment element with the unknown-size
// sentinel (width 8) followed by the body.
func ebmlUnknownSizeSegment(body []byte) []byte {
	out := ebmlID(0x18538067) // idSegment
	out = append(out, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	out = append(out, body...)
	return out
}
