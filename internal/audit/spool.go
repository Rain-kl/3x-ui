package audit

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
)

const MaxSpoolBytes = 10 << 20

var accessLine = regexp.MustCompile(`\bfrom (\S+) accepted (\S+)`)

// SummaryMetrics is used only when recovering a former standalone node's daily buckets.
type SummaryMetrics struct {
	Connections int64 `json:"connections"`
	Measured    int64 `json:"measured"`
}

type Event struct {
	Summary     *SummaryMetrics `json:"summary,omitempty"`
	ID          string          `json:"id"`
	Time        time.Time       `json:"time"`
	Inbound     string          `json:"inbound"`
	Outbound    string          `json:"outbound"`
	Client      string          `json:"client"`
	Source      string          `json:"source"`
	Destination string          `json:"destination"`
	// FORK(sniffed-audit-domains): Carry domain metadata through node spool and acknowledgement.
	SniffedDomain string `json:"sniffedDomain,omitempty"`
	Uplink        *int64 `json:"uplink,omitempty"`
	Downlink      *int64 `json:"downlink,omitempty"`
}

type Spool struct {
	mu     sync.Mutex
	path   string
	events []Event
	size   int
	dirty  bool
}

var (
	localOnce sync.Once
	local     *Spool
)

func Local() *Spool {
	localOnce.Do(func() {
		local = NewSpool(filepath.Join(config.GetLogFolder(), "audit-spool.jsonl"))
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				_ = local.Flush()
			}
		}()
	})
	return local
}

func NewSpool(path string) *Spool {
	s := &Spool{path: path}
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		var e Event
		if json.Unmarshal(scan.Bytes(), &e) == nil && e.ID != "" {
			s.appendLocked(e)
		}
	}
	if scan.Err() != nil || s.size > MaxSpoolBytes {
		s.dirty = true
	}
	return s
}

func encodedSize(e Event) int {
	b, _ := json.Marshal(e)
	return len(b) + 1
}

func (s *Spool) appendLocked(e Event) bool {
	n := encodedSize(e)
	if n > MaxSpoolBytes {
		return false
	}
	for s.size+n > MaxSpoolBytes && len(s.events) > 0 {
		s.size -= encodedSize(s.events[0])
		s.events = s.events[1:]
	}
	s.events = append(s.events, e)
	s.size += n
	s.dirty = true
	return true
}

func (s *Spool) Add(e Event) bool {
	if e.ID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return false
		}
		e.ID = hex.EncodeToString(id[:])
	}
	if e.Time.IsZero() || e.Destination == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendLocked(e)
}

func (s *Spool) Capture(line string) bool {
	match := accessLine.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	e := Event{Source: match[1], Destination: match[2], Time: time.Now().UTC()}
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		if t, err := time.ParseInLocation("2006/01/02 15:04:05.999999", fields[0]+" "+fields[1], time.Local); err == nil {
			e.Time = t.UTC()
		}
	}
	if left := strings.IndexByte(line, '['); left >= 0 {
		if right := strings.IndexByte(line[left:], ']'); right > 0 {
			route := strings.Fields(line[left+1 : left+right])
			if len(route) >= 3 {
				e.Inbound = route[0]
				e.Outbound = route[len(route)-1]
			}
		}
	}
	// FORK(audit-workbench): Consume internal API logs without filling the client audit spool.
	if e.Inbound == "api" {
		return true
	}
	for i, field := range fields {
		switch {
		case field == "email:" && i+1 < len(fields):
			e.Client = fields[i+1]
		// FORK(sniffed-audit-domains): Decode the core's escaped optional metadata token.
		case strings.HasPrefix(field, "sniffed_domain="):
			if domain, err := url.QueryUnescape(strings.TrimPrefix(field, "sniffed_domain=")); err == nil {
				e.SniffedDomain = domain
			}
		case strings.HasPrefix(field, "uplink="):
			if n, err := strconv.ParseInt(strings.TrimPrefix(field, "uplink="), 10, 64); err == nil && n >= 0 {
				e.Uplink = &n
			}
		case strings.HasPrefix(field, "downlink="):
			if n, err := strconv.ParseInt(strings.TrimPrefix(field, "downlink="), 10, 64); err == nil && n >= 0 {
				e.Downlink = &n
			}
		}
	}
	s.Add(e)
	return true
}

func (s *Spool) Snapshot(limit int) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	limit = min(limit, len(s.events))
	return append([]Event(nil), s.events[:limit]...)
}

func (s *Spool) Ack(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.events[:0]
	for _, e := range s.events {
		if _, ok := set[e.ID]; !ok {
			kept = append(kept, e)
		} else {
			s.size -= encodedSize(e)
			s.dirty = true
		}
	}
	s.events = kept
	return s.flushLocked()
}

func (s *Spool) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *Spool) flushLocked() error {
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".audit-spool-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	writer := bufio.NewWriter(f)
	for _, e := range s.events {
		b, _ := json.Marshal(e)
		if _, err := writer.Write(append(b, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

type OutputWriter struct {
	mu       sync.Mutex
	pending  []byte
	overflow bool
	fallback io.Writer
	spool    *Spool
}

func NewOutputWriter(fallback io.Writer) *OutputWriter {
	return &OutputWriter{fallback: fallback, spool: Local()}
}

func (w *OutputWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, data...)
	for {
		idx := -1
		for i, b := range w.pending {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := string(w.pending[:idx])
		w.pending = w.pending[idx+1:]
		if w.overflow || len(line) > 64<<10 {
			_, _ = io.WriteString(w.fallback, line+"\n")
			w.overflow = false
		} else if !w.spool.Capture(line) {
			_, _ = io.WriteString(w.fallback, line+"\n")
		}
	}
	if len(w.pending) > 64<<10 {
		_, _ = w.fallback.Write(w.pending)
		w.pending = w.pending[:0]
		w.overflow = true
	}
	return len(data), nil
}
