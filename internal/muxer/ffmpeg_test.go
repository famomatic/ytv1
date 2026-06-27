package muxer

import (
	"reflect"
	"testing"

	"github.com/famomatic/ytv1/internal/types"
)

func TestFFmpegMuxerMergeArgsIncludesExtraArgs(t *testing.T) {
	m := NewFFmpegMuxerWithExtraArgs("ffmpeg", []string{"-movflags", "+faststart"})
	got := m.mergeArgs("video.mp4", "audio.m4a", "out.mp4", types.Metadata{
		Title:  "Title",
		Artist: "Artist",
	})
	want := []string{
		"-protocol_whitelist", "file",
		"-i", "video.mp4",
		"-i", "audio.m4a",
		"-c:v", "copy",
		"-c:a", "copy",
		"-metadata", "title=Title",
		"-metadata", "artist=Artist",
		"-movflags", "+faststart",
		"-y", "out.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeArgs()=%v, want %v", got, want)
	}
}
