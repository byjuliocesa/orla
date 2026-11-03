package terminal

import (
	"sort"
	"sync"
	"time"

	"github.com/byjuliocesa/orla/internal/id"
)

// killGrace is how long a process gets after SIGHUP before it is killed.
const killGrace = 3 * time.Second

// Manager owns the running terminals. A terminal leaves it when its process exits.
type Manager struct {
	mu    sync.Mutex
	terms map[string]*Terminal
}

func NewManager() *Manager {
	return &Manager{terms: map[string]*Terminal{}}
}

// NewID returns an id for a terminal about to be opened, for settings that must know it first.
func NewID() string { return id.New("term") }

func (m *Manager) Open(spec Spec) (*Terminal, error) {
	return m.OpenWithID(NewID(), spec)
}

// OpenWithID opens a terminal under an id from NewID.
func (m *Manager) OpenWithID(termID string, spec Spec) (*Terminal, error) {
	t, err := start(termID, spec)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.terms[t.info.ID] = t
	m.mu.Unlock()

	go func() {
		<-t.Done()
		m.mu.Lock()
		delete(m.terms, t.info.ID)
		m.mu.Unlock()
	}()
	return t, nil
}

func (m *Manager) Get(id string) (*Terminal, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.terms[id]
	return t, ok
}

// List returns the running terminals, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	terms := make([]*Terminal, 0, len(m.terms))
	for _, t := range m.terms {
		terms = append(terms, t)
	}
	m.mu.Unlock()

	out := make([]Info, 0, len(terms))
	for _, t := range terms {
		out = append(out, t.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Close ends a terminal's process and waits for it to exit.
func (m *Manager) Close(id string) bool {
	t, ok := m.Get(id)
	if !ok {
		return false
	}
	t.kill(killGrace)
	return true
}

// CloseAll ends every terminal, for shutdown.
func (m *Manager) CloseAll() {
	var wg sync.WaitGroup
	for _, info := range m.List() {
		wg.Go(func() { m.Close(info.ID) })
	}
	wg.Wait()
}
