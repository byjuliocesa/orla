package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/byjuliocesa/orla/internal/events"
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestEventsSurviveReopenAndKeepCounting(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orla.db")

	bus := events.NewBus(open(t, path))
	for range 3 {
		if _, err := bus.Publish(ctx, events.Event{
			Session: "ses_a", Type: events.MessageUser, Data: []byte(`{"text":"hi"}`),
		}); err != nil {
			t.Fatal(err)
		}
	}

	reopened := open(t, path)
	e, err := events.NewBus(reopened).Publish(ctx, events.Event{Session: "ses_a", Type: events.Status})
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 4 {
		t.Fatalf("seq after reopen = %d, want 4", e.Seq)
	}

	got, err := reopened.After(ctx, "ses_a", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || string(got[0].Data) != `{"text":"hi"}` {
		t.Fatalf("stored events = %+v", got)
	}
}

func TestSessionsCountIndependently(t *testing.T) {
	ctx := context.Background()
	bus := events.NewBus(open(t, filepath.Join(t.TempDir(), "orla.db")))

	for _, session := range []string{"ses_a", "ses_b", "ses_a"} {
		if _, err := bus.Publish(ctx, events.Event{Session: session, Type: events.Status}); err != nil {
			t.Fatal(err)
		}
	}
	e, err := bus.Publish(ctx, events.Event{Session: "ses_b", Type: events.Status})
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 2 {
		t.Fatalf("ses_b seq = %d, want 2", e.Seq)
	}
}
