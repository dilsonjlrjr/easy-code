package event

import "sync"

// Broker fans notifications out to in-process subscribers. A subscriber that falls behind is
// dropped (its channel is closed) and resumes by replay: the broker never blocks the listener.
type Broker struct {
	mu          sync.Mutex
	subscribers map[*Subscription]struct{}
}

// Subscription receives the notifications of one project, or of all projects when ProjectID is empty.
type Subscription struct {
	ProjectID string
	ch        chan Notification
	closed    bool
}

// C is closed when the subscription ends: unsubscribed or dropped for being slow.
func (s *Subscription) C() <-chan Notification { return s.ch }

// NewBroker builds an empty broker.
func NewBroker() *Broker {
	return &Broker{subscribers: map[*Subscription]struct{}{}}
}

// Subscribe registers a subscriber with a buffer of the given size.
func (b *Broker) Subscribe(projectID string, buffer int) *Subscription {
	subscription := &Subscription{ProjectID: projectID, ch: make(chan Notification, max(buffer, 1))}
	b.mu.Lock()
	b.subscribers[subscription] = struct{}{}
	b.mu.Unlock()
	return subscription
}

// Unsubscribe removes the subscriber and closes its channel. Safe to call more than once.
func (b *Broker) Unsubscribe(subscription *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remove(subscription)
}

// Dispatch delivers n to every matching subscriber without blocking; it fits Listener.Listen.
func (b *Broker) Dispatch(n Notification) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for subscription := range b.subscribers {
		if subscription.ProjectID != "" && subscription.ProjectID != n.ProjectID {
			continue
		}
		select {
		case subscription.ch <- n:
		default:
			b.remove(subscription)
		}
	}
}

func (b *Broker) remove(subscription *Subscription) {
	if subscription.closed {
		return
	}
	subscription.closed = true
	delete(b.subscribers, subscription)
	close(subscription.ch)
}
