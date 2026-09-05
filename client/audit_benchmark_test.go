package client

import (
	"fmt"
	"github.com/famomatic/ytv1/internal/innertube"
	"testing"
)

func BenchmarkAuditCachedSession(b *testing.B) {
	c := New(Config{})
	resp := &innertube.PlayerResponse{}
	for i := 0; i < 200; i++ {
		resp.StreamingData.Formats = append(resp.StreamingData.Formats, innertube.Format{Itag: i + 1, URL: fmt.Sprintf("https://media.test/v?itag=%d&expire=4102444800", i)})
	}
	c.putSession("jNQXAC9IVRw", videoSession{Response: resp})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := c.getSession("jNQXAC9IVRw"); !ok {
			b.Fatal("cache miss")
		}
	}
}
