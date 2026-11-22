package event

import "testing"

func TestBrokerFiltersByProject(t *testing.T) {
	broker := NewBroker()
	mine := broker.Subscribe("p1", 4)
	all := broker.Subscribe("", 4)

	broker.Dispatch(Notification{ProjectID: "p1", Sequence: 1})
	broker.Dispatch(Notification{ProjectID: "p2", Sequence: 1})

	if got := len(mine.C()); got != 1 {
		t.Fatalf("project subscriber got %d notifications, want 1", got)
	}
	if got := len(all.C()); got != 2 {
		t.Fatalf("global subscriber got %d notifications, want 2", got)
	}
}

func TestBrokerDropsSlowSubscriber(t *testing.T) {
	broker := NewBroker()
	slow := broker.Subscribe("p1", 1)

	broker.Dispatch(Notification{ProjectID: "p1", Sequence: 1})
	broker.Dispatch(Notification{ProjectID: "p1", Sequence: 2})

	if n, ok := <-slow.C(); !ok || n.Sequence != 1 {
		t.Fatalf("first notification = %+v, %v", n, ok)
	}
	if _, ok := <-slow.C(); ok {
		t.Fatal("slow subscriber should have been dropped")
	}
	broker.Unsubscribe(slow) // idempotent after a drop
}
