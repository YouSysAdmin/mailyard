// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package eventbus

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPublishReachesOnlyItsProject(t *testing.T) {
	b := New()
	a, _ := b.Subscribe("proj-a")
	defer a.Close()
	other, _ := b.Subscribe("proj-b")
	defer other.Close()

	b.Publish(Event{Type: TypeEmailSent, ProjectID: "proj-a"})

	select {
	case e := <-a.C:
		if e.Type != TypeEmailSent {
			t.Errorf("type = %q, want %q", e.Type, TypeEmailSent)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber of proj-a received nothing")
	}

	// The bus is the tenancy boundary for the live stream, so this is
	// the assertion that matters most: a handler cannot leak another
	// project's activity by forgetting a filter, because it never
	// gets the event at all.
	select {
	case e := <-other.C:
		t.Fatalf("subscriber of proj-b received %q from proj-a", e.Type)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishDropsRatherThanBlocking(t *testing.T) {
	b := New()
	sub, _ := b.Subscribe("proj")
	defer sub.Close()

	// Nobody is reading. Publishing far past the buffer must return
	// promptly rather than wedging the caller - the publisher is the
	// delivery worker, and a stalled browser must not stall sending.
	done := make(chan struct{})
	go func() {
		for range buffer * 3 {
			b.Publish(Event{Type: TypeEmailSent, ProjectID: "proj"})
		}

		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}

	if _, _, dropped := b.Stats(); dropped == 0 {
		t.Error("dropped count = 0, want the overflow to be reported")
	}
}

func TestCloseIsIdempotentAndStopsDelivery(t *testing.T) {
	b := New()
	sub, _ := b.Subscribe("proj")
	sub.Close()
	sub.Close() // must not panic on a double close

	b.Publish(Event{Type: TypeEmailSent, ProjectID: "proj"})

	if _, ok := <-sub.C; ok {
		t.Error("received an event after Close")
	}

	if n, _, _ := b.Stats(); n != 0 {
		t.Errorf("subscribers after Close = %d, want 0", n)
	}
}

// Bus.Close is what lets a shutdown end SSE streams that would
// otherwise keep their connections active until the client hangs up.
// A live subscriber must observe its channel closing.
func TestBusCloseEndsLiveSubscriptions(t *testing.T) {
	b := New()
	a, _ := b.Subscribe("proj-a")
	c, _ := b.Subscribe("proj-b")

	b.Close()
	b.Close() // shutdown paths may run twice, must not panic

	for name, sub := range map[string]*Subscription{"proj-a": a, "proj-b": c} {
		select {
		case _, ok := <-sub.C:
			if ok {
				t.Errorf("%s: got an event, want the channel closed", name)
			}
		default:
			t.Errorf("%s: channel still open after Close, a stream would hang shutdown", name)
		}
	}

	if n, w, _ := b.Stats(); n != 0 || w != 0 {
		t.Errorf("after Close: subscribers=%d projects=%d, want 0/0", n, w)
	}

	// Unsubscribing a already-closed subscription must not double close.
	a.Close()
}

// A request that lands mid-shutdown must not get a live-looking
// subscription, or its handler would sit waiting for a heartbeat that
// nothing will send.
func TestSubscribeAfterCloseIsAlreadyFinished(t *testing.T) {
	b := New()
	b.Close()

	sub, _ := b.Subscribe("proj")
	select {
	case _, ok := <-sub.C:
		if ok {
			t.Error("got an event on a post-Close subscription")
		}
	default:
		t.Fatal("post-Close subscription is still open")
	}

	// Publishing to a closed bus must not panic on a closed channel.
	b.Publish(Event{Type: TypeEmailSent, ProjectID: "proj"})
	sub.Close()
}

func TestConcurrentPublishAndSubscribe(t *testing.T) {
	// Exists for the race detector: Publish increments the drop
	// counter while holding only a read lock, so several publishers
	// touch it at once by design.
	b := New()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			s, _ := b.Subscribe("proj")
			defer s.Close()
			for range 50 {
				b.Publish(Event{Type: TypeEmailSent, ProjectID: "proj"})
			}
		})
	}

	wg.Wait()
	if n, _, _ := b.Stats(); n != 0 {
		t.Errorf("subscribers left = %d, want 0", n)
	}
}

// A project, and the node, can hold only so many live streams - a
// stream is a held connection, and the route had no other bound.
func TestSubscribeRefusesPastTheCeiling(t *testing.T) {
	b := New()
	for i := 0; i < MaxSubscribersPerProject; i++ {
		if _, err := b.Subscribe("proj"); err != nil {
			t.Fatalf("subscription %d refused: %v", i, err)
		}
	}

	if _, err := b.Subscribe("proj"); !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("past the per-project ceiling: got %v, want ErrTooManySubscribers", err)
	}

	// Another project is unaffected by the first one's ceiling.
	if _, err := b.Subscribe("other"); err != nil {
		t.Fatalf("a second project refused: %v", err)
	}
}
