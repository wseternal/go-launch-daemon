package eventbus

import (
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe(TopicState)

	event := Event{
		Topic: TopicState,
		Type:  "state",
		Name:  "svc",
	}
	bus.Publish(event)

	select {
	case got := <-ch:
		if got.Name != "svc" {
			t.Fatalf("expected name svc, got %q", got.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestTopicFilter(t *testing.T) {
	bus := New()
	defer bus.Close()

	stateCh := bus.Subscribe(TopicState)
	logCh := bus.Subscribe(TopicLog)

	bus.Publish(Event{Topic: TopicState, Type: "state", Name: "svc"})
	bus.Publish(Event{Topic: TopicLog, Type: "log", Name: "svc", Data: "hello"})

	// State channel should only get state events.
	select {
	case e := <-stateCh:
		if e.Topic != TopicState {
			t.Fatalf("stateCh got wrong topic: %s", e.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout on stateCh")
	}

	// Log channel should only get log events.
	select {
	case e := <-logCh:
		if e.Topic != TopicLog {
			t.Fatalf("logCh got wrong topic: %s", e.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout on logCh")
	}
}

func TestNonBlockingPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	// Subscribe but never drain.
	_ = bus.Subscribe(TopicState)

	start := time.Now()
	for range 10000 {
		bus.Publish(Event{Topic: TopicState, Type: "state"})
	}
	elapsed := time.Since(start)

	// 10k publishes should complete in well under 100ms.
	if elapsed > 100*time.Millisecond {
		t.Fatalf("10k publishes took %v, expected < 100ms", elapsed)
	}
}

func TestDropCount(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe(TopicState)

	// Fill the buffer (128) + some drops.
	for range 200 {
		bus.Publish(Event{Topic: TopicState, Type: "state"})
	}

	// Drain the channel.
	drained := 0
	for {
		select {
		case <-ch:
			drained++
		default:
			goto done
		}
	}
done:

	// Drops = 200 - drained (some may include resync events).
	if drained < subscriberBufSize {
		t.Fatalf("expected at least %d drained, got %d", subscriberBufSize, drained)
	}
}

func TestResyncAfterDrops(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe(TopicState)

	// Overflow the buffer to cause drops.
	for range 200 {
		bus.Publish(Event{Topic: TopicState, Type: "state"})
	}

	// Drain the channel to make room.
	for {
		select {
		case <-ch:
		default:
			goto drained
		}
	}
drained:

	// Now publish again — this should inject a resync event.
	bus.Publish(Event{Topic: TopicState, Type: "state", Name: "after-drain"})

	// Drain and look for resync.
	events := drainChannel(ch)
	hasResync := false
	for _, e := range events {
		if e.Type == "resync" {
			hasResync = true
			break
		}
	}
	if !hasResync {
		t.Fatal("expected resync event after drops + drain + publish")
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe(TopicState)
	bus.Unsubscribe(ch)

	// Channel should be closed.
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed")
	}
}

func TestClose(t *testing.T) {
	bus := New()

	ch1 := bus.Subscribe(TopicState)
	ch2 := bus.Subscribe(TopicLog)

	bus.Close()

	// All channels should be closed.
	_, ok1 := <-ch1
	_, ok2 := <-ch2

	if ok1 || ok2 {
		t.Fatal("expected all channels to be closed")
	}

	// Publish after close is a no-op.
	bus.Publish(Event{Topic: TopicState, Type: "state"})
}

func TestConcurrentPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe(TopicState)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				bus.Publish(Event{Topic: TopicState, Type: "state"})
			}
		}()
	}

	// Drain in background.
	go func() {
		for range ch {
		}
	}()

	wg.Wait()
	bus.Unsubscribe(ch)
}

func TestMultipleSubscribers(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch1 := bus.Subscribe(TopicState)
	ch2 := bus.Subscribe(TopicState)

	bus.Publish(Event{Topic: TopicState, Type: "state", Name: "svc"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Name != "svc" {
				t.Fatalf("expected svc, got %q", e.Name)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
}

func drainChannel(ch <-chan Event) []Event {
	var events []Event
	for {
		select {
		case e := <-ch:
			events = append(events, e)
		default:
			return events
		}
	}
}
