package debugstream

import (
	"encoding/json"
	"sync"
)

type Bus struct {
	queueSize   int
	historySize int
	closed      bool
	history     []string
	subscribers map[chan string]struct{}
	mu          sync.Mutex
}

func NewBus() *Bus {
	return &Bus{
		queueSize:   1000,
		historySize: 1000,
		subscribers: make(map[chan string]struct{}),
	}
}

func (b *Bus) Subscribe() chan string {
	subscriber := make(chan string, b.queueSize)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(subscriber)
		return subscriber
	}
	for _, line := range b.history {
		select {
		case subscriber <- line:
		default:
			break
		}
	}
	b.subscribers[subscriber] = struct{}{}
	return subscriber
}

func (b *Bus) Unsubscribe(subscriber chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subscribers[subscriber]; ok {
		delete(b.subscribers, subscriber)
		close(subscriber)
	}
}

func (b *Bus) Publish(event map[string]any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	line := string(data) + "\n"

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.history = append(b.history, line)
	if len(b.history) > b.historySize {
		b.history = b.history[len(b.history)-b.historySize:]
	}
	subscribers := make([]chan string, 0, len(b.subscribers))
	for subscriber := range b.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	b.mu.Unlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- line:
		default:
		}
	}
}

func (b *Bus) History() []map[string]any {
	b.mu.Lock()
	lines := append([]string(nil), b.history...)
	b.mu.Unlock()

	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			events = append(events, event)
		}
	}
	return events
}

func (b *Bus) ClearHistory() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.history = nil
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for subscriber := range b.subscribers {
		close(subscriber)
		delete(b.subscribers, subscriber)
	}
}

func (b *Bus) IsClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}
