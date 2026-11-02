package events

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Julio0Cesar/orla/internal/id"
)

// Log is where published events are kept. Append assigns the next seq of the event's session.
type Log interface {
	Append(ctx context.Context, e Event) (int64, error)
	After(ctx context.Context, session string, after int64, limit int) ([]Event, error)
}

// subscriberBuffer is how many events a subscriber may fall behind before it is dropped.
const subscriberBuffer = 1024

// replayPage is how many stored events are read at a time while catching a subscriber up.
const replayPage = 500

type subscriber struct {
	session string
	live    chan Event
}

// Bus stores events and fans them out to subscribers of their session.
//
// Guarantee: a subscriber receives every event of its session after the seq it asked for, in
// seq order, without gaps or duplicates, until its channel closes. A channel that closes before
// the subscriber cancelled means it fell too far behind; it subscribes again from the last seq
// it saw. Slow subscribers are dropped instead of slowing down publishers.
type Bus struct {
	log Log

	// mu serializes publishing, so seq order and delivery order are the same, and makes
	// registering a subscriber atomic with respect to publishing, so none falls in between.
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func NewBus(log Log) *Bus {
	return &Bus{log: log, subs: map[*subscriber]struct{}{}}
}

// Publish stores the event and delivers it. ID, Seq and Time are filled in when empty.
func (b *Bus) Publish(ctx context.Context, e Event) (Event, error) {
	if e.Session == "" || e.Type == "" {
		return Event{}, errors.New("events: session and type are required")
	}
	if e.ID == "" {
		e.ID = id.New("evt")
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	seq, err := b.log.Append(ctx, e)
	if err != nil {
		return Event{}, err
	}
	e.Seq = seq

	for s := range b.subs {
		if s.session != e.Session {
			continue
		}
		select {
		case s.live <- e:
		default:
			delete(b.subs, s)
			close(s.live)
		}
	}
	return e, nil
}

// Subscribe returns the session's events with seq greater than after: first the stored ones,
// then live ones. The channel closes when ctx ends or the subscriber falls behind.
func (b *Bus) Subscribe(ctx context.Context, session string, after int64) <-chan Event {
	s := &subscriber{session: session, live: make(chan Event, subscriberBuffer)}

	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	out := make(chan Event)
	go func() {
		defer close(out)
		defer b.unsubscribe(s)

		last := after
		send := func(e Event) bool {
			// Replay and live overlap for events published while replaying; seq tells them apart.
			if e.Seq <= last {
				return true
			}
			select {
			case out <- e:
				last = e.Seq
				return true
			case <-ctx.Done():
				return false
			}
		}

		for {
			page, err := b.log.After(ctx, session, last, replayPage)
			if err != nil {
				return
			}
			for _, e := range page {
				if !send(e) {
					return
				}
			}
			if len(page) < replayPage {
				break
			}
		}

		for {
			select {
			case e, ok := <-s.live:
				if !ok || !send(e) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func (b *Bus) unsubscribe(s *subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.live)
	}
}
