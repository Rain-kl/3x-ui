package panelai

import (
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type (
	AIStream struct {
		Run      model.AIRun `json:"run"`
		Thought  string      `json:"thought"`
		Sequence int64       `json:"sequence"`
	}
	runStream struct {
		mu       sync.Mutex
		value    AIStream
		watchers map[chan AIStream]bool
	}
)

func (m *Manager) streamFor(id string) *runStream {
	value, _ := m.streams.LoadOrStore(id, &runStream{watchers: map[chan AIStream]bool{}})
	return value.(*runStream)
}
func (s *runStream) snapshot() AIStream { s.mu.Lock(); defer s.mu.Unlock(); return s.value }
func (s *runStream) publish(value AIStream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value.Sequence = s.value.Sequence + 1
	s.value = value
	for watcher := range s.watchers {
		select {
		case <-watcher:
		default:
		}
		watcher <- value
	}
}

func (m *Manager) Watch(id string) (<-chan AIStream, func(), error) {
	// Subscribe before reading durable state so completion cannot strand a reconnect on a stale snapshot.
	value, loaded := m.streams.LoadOrStore(id, &runStream{watchers: map[chan AIStream]bool{}})
	stream := value.(*runStream)
	run, err := Run(id)
	if err != nil {
		if !loaded {
			m.streams.CompareAndDelete(id, stream)
		}
		return nil, nil, err
	}
	stream.mu.Lock()
	if stream.value.Sequence == 0 {
		stream.value = AIStream{Run: *run, Sequence: 1}
	}
	channel := make(chan AIStream, 1)
	channel <- stream.value
	stream.watchers[channel] = true
	stream.mu.Unlock()
	return channel, func() {
		stream.mu.Lock()
		delete(stream.watchers, channel)
		stream.mu.Unlock()
		if run.State != "running" {
			m.streams.CompareAndDelete(id, stream)
		}
	}, nil
}
