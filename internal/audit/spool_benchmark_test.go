package audit

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func BenchmarkSpoolFlush(b *testing.B) {
	s := NewSpool(filepath.Join(b.TempDir(), "spool.jsonl"))
	for i := range 10000 {
		s.Add(Event{ID: fmt.Sprint(i), Time: time.Now().UTC(), Destination: strings.Repeat("x", 600)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Add(Event{ID: fmt.Sprintf("extra-%d", i), Time: time.Now().UTC(), Destination: "example.com"})
		if err := s.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}
