package daemon

import "testing"

func TestEventHubPublishSubscribeAndUnsubscribe(t *testing.T) {
	hub := newEventHub()
	ch, unsubscribe := hub.subscribe()

	hub.publish("deploy:status", map[string]string{"status": "running"})
	ev := <-ch
	if ev.Name != "deploy:status" {
		t.Fatalf("event name = %q, want deploy:status", ev.Name)
	}

	unsubscribe()
	hub.publish("deploy:status", nil)
	if _, ok := <-ch; ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}

func TestEventHubDropsWhenSubscriberBackedUp(t *testing.T) {
	hub := newEventHub()
	ch, unsubscribe := hub.subscribe()
	defer unsubscribe()

	for i := 0; i < cap(ch)+10; i++ {
		hub.publish("event", i)
	}

	if len(ch) != cap(ch) {
		t.Fatalf("buffered events = %d, want capped at %d", len(ch), cap(ch))
	}
}
