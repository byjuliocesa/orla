package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Watcher reports which files change in a work tree while an agent works in it.
//
// It reads git status, which git answers from its index in milliseconds, instead of watching
// the file system: inotify needs a watch per directory, and a repository with a node_modules
// runs out of them.
type Watcher struct {
	root    string
	every   time.Duration
	changed func(paths []string)
	prev    map[string]string

	nudge    chan struct{}
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// maxWatched bounds the files compared on each check.
const maxWatched = 5000

// Watch takes the work tree that holds dir as it is now, the state later changes are measured
// from, and returns a watcher for Run. It returns ErrNotRepo outside a git repository.
func Watch(ctx context.Context, dir string, every time.Duration, changed func(paths []string)) (*Watcher, error) {
	repo, err := Open(ctx, dir)
	if err != nil {
		return nil, err
	}
	prev, err := snapshot(ctx, repo.Root)
	if err != nil {
		return nil, err
	}
	return &Watcher{
		root: repo.Root, every: every, changed: changed, prev: prev,
		nudge: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

// Nudge asks for a check now, such as after a tool call that may have written files.
func (w *Watcher) Nudge() {
	select {
	case w.nudge <- struct{}{}:
	default:
	}
}

// Stop has Run check once more and return, and waits for it, so the last changes are reported
// before Stop returns.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() { close(w.stop) })
	<-w.done
}

// Run checks the work tree every interval and on each nudge, until Stop or ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	defer close(w.done)
	tick := time.NewTicker(w.every)
	defer tick.Stop()
	for {
		last := false
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-w.nudge:
		case <-w.stop:
			last = true
		}
		cur, err := snapshot(ctx, w.root)
		if err == nil {
			if paths := compare(w.prev, cur); len(paths) > 0 {
				w.changed(paths)
			}
			w.prev = cur
		}
		if last {
			return
		}
	}
}

// snapshot is each changed path with its status, size and modification time: a file edited
// again keeps its status but not its time.
func snapshot(ctx context.Context, root string) (map[string]string, error) {
	entries, err := status(ctx, root)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxWatched {
		entries = entries[:maxWatched]
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		mark := e.code
		if info, err := os.Lstat(filepath.Join(root, e.path)); err == nil {
			mark += " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		}
		out[e.path] = mark
	}
	return out, nil
}

func compare(prev, cur map[string]string) []string {
	var paths []string
	for p, m := range cur {
		if prev[p] != m {
			paths = append(paths, p)
		}
	}
	for p := range prev {
		if _, ok := cur[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	return paths
}
