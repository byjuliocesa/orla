package events_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/store"
)

func newBus(t *testing.T) *events.Bus {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "orla.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return events.NewBus(s)
}

func publish(t *testing.T, bus *events.Bus, session string) {
	t.Helper()
	if _, err := bus.Publish(context.Background(), events.Event{Session: session, Type: events.Status}); err != nil {
		t.Error(err)
	}
}

// A client that reconnects while events keep arriving must get every one after the seq it
// last saw, in order, with no gap and no duplicate.
func TestSubscribeDuringPublishingMissesNothing(t *testing.T) {
	bus := newBus(t)
	for range 50 {
		publish(t, bus, "ses_a")
	}

	const writers, each = 4, 50
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for range each {
				publish(t, bus, "ses_a")
				publish(t, bus, "ses_other")
			}
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch := bus.Subscribe(ctx, "ses_a", 20)

	want := int64(21)
	for want <= 50+writers*each {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("closed before seq %d", want)
			}
			if e.Session != "ses_a" || e.Seq != want {
				t.Fatalf("got %s seq %d, want ses_a seq %d", e.Session, e.Seq, want)
			}
			want++
		case <-ctx.Done():
			t.Fatalf("timed out waiting for seq %d", want)
		}
	}
	wg.Wait()
}

// A subscriber that stops reading must not block publishers; it is cut off instead, and its
// channel closes so it knows to resubscribe.
func TestStalledSubscriberDoesNotBlockPublishers(t *testing.T) {
	bus := newBus(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := bus.Subscribe(ctx, "ses_a", 0)
	publish(t, bus, "ses_a")
	if e := <-ch; e.Seq != 1 {
		t.Fatalf("first seq = %d", e.Seq)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 3000 {
			publish(t, bus, "ses_a")
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("publishers blocked by a subscriber that stopped reading")
	}

	// It may still drain what was buffered before being cut off, then the channel closes.
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("stalled subscriber was never closed")
		}
	}
}
