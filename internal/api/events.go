package api

import (
	"sync"
)

// TrackerEvent is broadcast whenever stored credentials for one tracker
// change, so connected clients can refresh immediately instead of polling.
type TrackerEvent struct {
	Type    string `json:"type"`
	Tracker string `json:"tracker"`
}

const trackerEventCredentials = "credentials"

// trackerBroker distributes credential-change events to websocket
// subscribers. Delivery is best effort: a slow subscriber drops events and
// reconciles through its own refetch.
type trackerBroker struct {
	mu          sync.Mutex
	nextID      int
	subscribers map[int]chan TrackerEvent
}

func newTrackerBroker() *trackerBroker {
	return &trackerBroker{subscribers: map[int]chan TrackerEvent{}}
}

func (b *trackerBroker) subscribe() (<-chan TrackerEvent, func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	channel := make(chan TrackerEvent, 8)
	b.subscribers[id] = channel
	b.mu.Unlock()
	return channel, func() {
		b.mu.Lock()
		if current, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(current)
		}
		b.mu.Unlock()
	}
}

func (b *trackerBroker) publish(event TrackerEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, channel := range b.subscribers {
		select {
		case channel <- event:
		default:
		}
	}
}
