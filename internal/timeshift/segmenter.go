// Package timeshift turns a continuous live MPEG-TS byte stream into a seekable
// local HLS "DVR": a sliding window of keyframe-aligned segments plus a media
// playlist, served over HTTP. A player pointed at the playlist can seek backward
// within the window (past seeking) even though the source is live and unbounded.
//
// It is source-agnostic — it consumes a plain MPEG-TS stream, so any producer
// (the SOOP P2P agent muxer, a demuxed YouTube live stream, an ffmpeg pipe, …)
// can feed it. The segmenter never re-times or re-muxes; it only cuts the stream
// at keyframe boundaries, so presentation timestamps stay exactly as produced.
package timeshift

import (
	"fmt"
	"time"
)

const tsPacketLen = 188

// Segment is one independently decodable MPEG-TS chunk: the latest PAT+PMT
// followed by a video keyframe and the packets up to (not including) the next
// segment boundary.
type Segment struct {
	Seq           int
	Data          []byte
	Duration      time.Duration
	Discontinuity bool
}

// TSSegmenter splits a continuous MPEG-TS stream (written to it as an io.Writer)
// into keyframe-aligned Segments, invoking onSegment for each completed one. It
// discovers the PMT and video PID from the stream itself (so it is not tied to
// one muxer's PID assignment) and caches the latest PAT/PMT to prepend to every
// segment, keeping each segment self-contained.
type TSSegmenter struct {
	MaxSegmentBytes int64 // 0 uses the 64 MiB safety limit
	pending, pes    []byte
	codec           byte
	discontinuity   bool
	target          time.Duration
	onSegment       func(Segment)

	buf []byte // unparsed tail carried across Write calls (packet reassembly)

	patPkt []byte // latest PAT packet (188 bytes), prepended to each segment
	pmtPkt []byte // latest PMT packet
	pmtPID int    // -1 until learned from the PAT
	vidPID int    // -1 until learned from the PMT

	cur     []byte // current segment bytes (starts with PAT+PMT)
	seq     int    // next segment sequence number
	started bool   // a segment is open (first keyframe seen)
	segPTS  int64  // PTS (90kHz) of the current segment's first keyframe
	havePTS bool   // segPTS is valid
}

// NewTSSegmenter returns a segmenter that cuts a new segment at the first video
// keyframe at or after target has elapsed since the current segment started.
func NewTSSegmenter(target time.Duration, onSegment func(Segment)) *TSSegmenter {
	return &TSSegmenter{target: target, onSegment: onSegment, pmtPID: -1, vidPID: -1}
}

// Write consumes a chunk of the MPEG-TS byte stream. It is safe to call with
// arbitrarily sized chunks; packets are reassembled internally.
func (s *TSSegmenter) Write(p []byte) (int, error) {
	original := len(p)
	for len(p) > 0 {
		n := min(tsPacketLen-len(s.buf), len(p))
		s.buf = append(s.buf, p[:n]...)
		p = p[n:]
		if len(s.buf) < tsPacketLen {
			break
		}
		if s.buf[0] != 0x47 {
			s.buf = s.buf[1:]
			continue
		}
		s.handlePacket(s.buf)
		s.buf = s.buf[:0]
		limit := s.MaxSegmentBytes
		if limit <= 0 {
			limit = 64 << 20
		}
		if int64(len(s.cur)+len(s.pending)) > limit {
			return original - len(p), fmt.Errorf("DVR segment exceeds %d bytes without a usable keyframe", limit)
		}
	}
	return original, nil
}

// Close flushes the final open segment (its true duration is unknown without a
// following keyframe, so target is used as an estimate).
func (s *TSSegmenter) Close() {
	s.consumePES()
	s.flush(s.segPTS + int64(s.target.Seconds()*90000))
}

func (s *TSSegmenter) handlePacket(pkt []byte) {
	pid := (int(pkt[1]&0x1F) << 8) | int(pkt[2])
	pusi := pkt[1]&0x40 != 0

	switch {
	case pid == 0: // PAT
		s.patPkt = append(s.patPkt[:0], pkt...)
		if pusi {
			s.pmtPID = parsePATFirstPMTPID(payloadOf(pkt, true))
		}
	case pid == s.pmtPID && s.pmtPID >= 0: // PMT
		s.pmtPkt = append(s.pmtPkt[:0], pkt...)
		if pusi {
			s.vidPID, s.codec = parsePMTVideo(payloadOf(pkt, true))
		}
	}
	if pid == s.vidPID && s.vidPID >= 0 && pusi {
		s.consumePES()
	}
	if pid == s.vidPID && s.vidPID >= 0 {
		payload := payloadOf(pkt, false)
		if len(s.pes)+len(payload) <= 1<<20 {
			s.pes = append(s.pes, payload...)
		}
	}
	if len(s.pes) > 0 {
		s.pending = append(s.pending, pkt...)
	} else if s.started {
		s.cur = append(s.cur, pkt...)
	}

}

func (s *TSSegmenter) consumePES() {
	if len(s.pending) == 0 {
		return
	}
	if isKeyframeCodec(s.pes, s.codec) {
		pts, ok := pesPTS(s.pes)
		s.onKeyframe(pts, ok)
	}
	if s.started {
		s.cur = append(s.cur, s.pending...)
	}
	s.pending = nil
	s.pes = nil
}

func ptsDelta(a, b int64) int64 {
	const wrap = int64(1) << 33
	delta := (a - b + wrap/2) % wrap
	if delta < 0 {
		delta += wrap
	}
	return delta - wrap/2
}

// onKeyframe handles a video keyframe access unit at pts (90kHz). It opens the
// first segment, and thereafter cuts a new segment once target has elapsed.
func (s *TSSegmenter) onKeyframe(pts int64, ptsOK bool) {
	if !s.started {
		s.beginSegment(pts, ptsOK)
		return
	}
	if !ptsOK || !s.havePTS {
		s.flush(s.segPTS + int64(s.target.Seconds()*90000))
		s.beginSegment(pts, ptsOK)
		return
	}
	delta := ptsDelta(pts, s.segPTS)
	elapsed := time.Duration(float64(delta) / 90000 * float64(time.Second))
	if delta < 0 || elapsed > 10*s.target {
		s.flush(s.segPTS + int64(s.target.Seconds()*90000))
		s.discontinuity = true
		s.beginSegment(pts, ptsOK)
	} else if elapsed >= s.target {
		s.flush(s.segPTS + delta)
		s.beginSegment(pts, ptsOK)
	}

}

// beginSegment starts a new segment buffer seeded with the latest PAT+PMT so the
// segment is self-contained. The current keyframe packet is appended by the
// normal accumulation path in handlePacket.
func (s *TSSegmenter) beginSegment(pts int64, ptsOK bool) {
	s.cur = nil
	if s.patPkt != nil {
		s.cur = append(s.cur, s.patPkt...)
	}
	if s.pmtPkt != nil {
		s.cur = append(s.cur, s.pmtPkt...)
	}
	s.started = true
	s.segPTS = pts
	s.havePTS = ptsOK
}

// flush emits the current segment with the duration endPTS-segPTS.
func (s *TSSegmenter) flush(endPTS int64) {
	if !s.started || len(s.cur) == 0 {
		return
	}
	dur := s.target
	if s.havePTS && endPTS > s.segPTS {
		dur = time.Duration(float64(endPTS-s.segPTS) / 90000.0 * float64(time.Second))
	}
	seg := Segment{Seq: s.seq, Data: append([]byte(nil), s.cur...), Duration: dur, Discontinuity: s.discontinuity}
	s.discontinuity = false
	s.seq++
	s.started = false
	s.cur = nil
	if s.onSegment != nil {
		s.onSegment(seg)
	}
}
