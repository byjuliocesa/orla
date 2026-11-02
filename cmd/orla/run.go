package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/Julio0Cesar/orla/internal/events"
)

type sessionInfo struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Cwd       string    `json:"cwd"`
	ActiveRun string    `json:"active_run"`
	LastSeq   int64     `json:"last_seq"`
	Created   time.Time `json:"created"`
}

func sessions(args []string) error {
	fs, connect := clientFlags("sessions")
	_ = fs.Parse(args)
	c, err := connect()
	if err != nil {
		return err
	}
	var res struct{ Sessions []sessionInfo }
	if err := c.do("GET", "/v1/sessions", nil, &res); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tAGENT\tCWD\tRUNNING\tCREATED")
	for _, s := range res.Sessions {
		running := ""
		if s.ActiveRun != "" {
			running = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Agent, s.Cwd, running,
			s.Created.Local().Format(time.DateTime))
	}
	return w.Flush()
}

func run(args []string) error {
	fs, connect := clientFlags("run")
	sessionID := fs.String("session", "", "continue this session; empty starts a new one")
	cwd := fs.String("cwd", "", "folder for a new session (default: current folder)")
	mode := fs.String("mode", "", "permission mode, as the CLI names it (e.g. manual, acceptEdits, auto, plan); empty keeps the CLI's default")
	model := fs.String("model", "", "model for this run, as the CLI names it (e.g. haiku, sonnet)")
	raw := fs.Bool("json", false, "print events as JSON lines instead of a timeline")
	_ = fs.Parse(args)
	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		return errors.New(`usage: orla run [-session id] [-cwd dir] [-mode m] "prompt"`)
	}
	c, err := connect()
	if err != nil {
		return err
	}

	var sess sessionInfo
	if *sessionID == "" {
		dir := *cwd
		if dir == "" {
			dir, _ = os.Getwd()
		}
		if err := c.do("POST", "/v1/sessions", map[string]string{"agent": "claude", "cwd": dir}, &sess); err != nil {
			return err
		}
	} else {
		var res struct{ Session sessionInfo }
		if err := c.do("GET", "/v1/sessions/"+*sessionID, nil, &res); err != nil {
			return err
		}
		sess = res.Session
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Listen before starting, from the last event seen, so nothing of the run is missed.
	stream, err := c.events(ctx, sess.ID, sess.LastSeq)
	if err != nil {
		return err
	}
	defer stream.Close()

	var started struct{ ID string }
	if err := c.do("POST", "/v1/sessions/"+sess.ID+"/runs",
		map[string]string{"prompt": prompt, "permission_mode": *mode, "model": *model}, &started); err != nil {
		return err
	}

	// Ctrl-C asks the server to cancel, then waits for the run to report how it ended.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	go func() {
		for range interrupt {
			fmt.Fprintln(os.Stderr, "\ncancelling…")
			_ = c.do("POST", "/v1/sessions/"+sess.ID+"/cancel", nil, nil)
		}
	}()

	r := newRenderer(term.IsTerminal(int(os.Stdout.Fd())))
	sc := bufio.NewScanner(stream)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var e events.Event
		if err := json.Unmarshal([]byte(line[len("data: "):]), &e); err != nil || e.Run != started.ID {
			continue
		}
		if *raw {
			fmt.Println(line[len("data: "):])
		} else {
			r.render(e)
		}
		if e.Type == events.RunFinished {
			if !*raw {
				fmt.Printf("%s\n", r.dim(`continue: orla run -session `+sess.ID+` "..."`))
			}
			return nil
		}
	}
	return errors.New("the event stream ended before the run finished; see: orla sessions")
}

func (c *client) events(ctx context.Context, session string, after int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"http://"+c.addr+"/v1/sessions/"+session+"/events?after="+strconv.FormatInt(after, 10), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("event stream: %s", res.Status)
	}
	return res.Body, nil
}

// watch follows a session's timeline as it happens, whoever drives it: a headless run, or
// Claude Code typed into an Orla terminal.
func watch(args []string) error {
	fs, connect := clientFlags("watch")
	all := fs.Bool("all", false, "replay the session's history first")
	raw := fs.Bool("json", false, "print events as JSON lines")
	_ = fs.Parse(args)
	c, err := connect()
	if err != nil {
		return err
	}

	var sess sessionInfo
	if fs.NArg() > 0 {
		var res struct{ Session sessionInfo }
		if err := c.do("GET", "/v1/sessions/"+fs.Arg(0), nil, &res); err != nil {
			return err
		}
		sess = res.Session
	} else {
		var res struct{ Sessions []sessionInfo }
		if err := c.do("GET", "/v1/sessions", nil, &res); err != nil {
			return err
		}
		if len(res.Sessions) == 0 {
			return errors.New("no sessions yet")
		}
		sess = res.Sessions[0]
	}
	from := sess.LastSeq
	if *all {
		from = 0
	}
	fmt.Fprintf(os.Stderr, "%s\n", newRenderer(false).dim("watching "+sess.ID+" in "+sess.Cwd+" (Ctrl-C to stop)"))

	stream, err := c.events(context.Background(), sess.ID, from)
	if err != nil {
		return err
	}
	defer stream.Close()
	r := newRenderer(term.IsTerminal(int(os.Stdout.Fd())))
	sc := bufio.NewScanner(stream)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		if *raw {
			fmt.Println(line)
			continue
		}
		var e events.Event
		if json.Unmarshal([]byte(line), &e) == nil {
			r.render(e)
		}
	}
	return errors.New("event stream closed; run watch again to resume")
}
