package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"

	"github.com/Julio0Cesar/orla/internal/terminal"
)

// detachKey is Ctrl-], the telnet escape: rarely bound by shells or editors, unlike Ctrl-\ or
// Ctrl-D.
const detachKey = 0x1d

type client struct {
	addr  string
	token string
}

func clientFlags(name string) (*flag.FlagSet, func() (*client, error)) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "server address")
	dataDir := fs.String("data", defaultDataDir(), "server data directory, to read the token from")
	return fs, func() (*client, error) {
		// ORLA_TOKEN reaches a server on another machine; otherwise the server is local and
		// its token file is readable by the same user.
		token := os.Getenv("ORLA_TOKEN")
		if token == "" {
			b, err := os.ReadFile(tokenPath(*dataDir))
			if err != nil {
				return nil, fmt.Errorf("no ORLA_TOKEN and no token file: %w", err)
			}
			token = strings.TrimSpace(string(b))
		}
		return &client{addr: *addr, token: token}, nil
	}
}

func (c *client) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://"+c.addr+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct{ Error string }
		b, _ := io.ReadAll(res.Body)
		if json.Unmarshal(b, &e) != nil || e.Error == "" {
			e.Error = strings.TrimSpace(string(b))
		}
		return fmt.Errorf("%s: %s", res.Status, e.Error)
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func list(args []string) error {
	fs, connect := clientFlags("ls")
	_ = fs.Parse(args)
	c, err := connect()
	if err != nil {
		return err
	}
	var res struct{ Terminals []terminal.Info }
	if err := c.do("GET", "/v1/terminals", nil, &res); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCOMMAND\tCWD\tSTARTED\tCLIENTS")
	for _, t := range res.Terminals {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", t.ID, strings.Join(t.Argv, " "), t.Cwd,
			t.Started.Local().Format(time.DateTime), t.Clients)
	}
	return w.Flush()
}

func closeTerminal(args []string) error {
	fs, connect := clientFlags("close")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: orla close <id>")
	}
	c, err := connect()
	if err != nil {
		return err
	}
	return c.do("DELETE", "/v1/terminals/"+fs.Arg(0), nil, nil)
}

func attach(args []string) error {
	// Split off the command first: flag.Parse swallows "--", after which an id and a command
	// look the same.
	var command []string
	for i, a := range args {
		if a == "--" {
			args, command = args[:i], args[i+1:]
			break
		}
	}
	fs, connect := clientFlags("attach")
	_ = fs.Parse(args)
	if fs.NArg() > 1 || (fs.NArg() == 1 && len(command) > 0) {
		return errors.New("usage: orla attach [id] | orla attach [-- command...]")
	}
	c, err := connect()
	if err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("attach needs an interactive terminal")
	}
	cols, rows, _ := term.GetSize(int(os.Stdout.Fd()))

	id := fs.Arg(0)
	if id == "" {
		cwd, _ := os.Getwd()
		var info terminal.Info
		spec := terminal.Spec{Argv: command, Cwd: cwd, Cols: uint16(cols), Rows: uint16(rows)}
		if err := c.do("POST", "/v1/terminals", spec, &info); err != nil {
			return err
		}
		id = info.ID
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+c.addr+"/v1/terminals/"+id+"/stream",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}}})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)

	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	restore := func() { _ = term.Restore(int(os.Stdin.Fd()), old) }
	defer restore()

	resize := func() {
		if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			msg, _ := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
			_ = conn.Write(ctx, websocket.MessageText, msg)
		}
	}
	// An existing terminal may have been sized for another screen.
	resize()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			resize()
		}
	}()

	detached := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			p := buf[:n]
			if i := bytes.IndexByte(p, detachKey); i >= 0 {
				if i > 0 {
					_ = conn.Write(ctx, websocket.MessageBinary, p[:i])
				}
				close(detached)
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, p); err != nil {
				return
			}
		}
	}()

	output := make(chan string, 1)
	go func() { output <- pumpOutput(ctx, conn) }()

	var note string
	select {
	case note = <-output:
	case <-detached:
		conn.Close(websocket.StatusNormalClosure, "detached")
		note = fmt.Sprintf("detached; the terminal keeps running. Return with: orla attach %s", id)
	}
	restore()
	fmt.Fprintf(os.Stderr, "\r\n[%s]\r\n", note)
	return nil
}

// pumpOutput copies terminal output to stdout and returns why the stream ended.
func pumpOutput(ctx context.Context, conn *websocket.Conn) string {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			switch websocket.CloseStatus(err) {
			case websocket.StatusNormalClosure:
				return "terminal closed"
			case websocket.StatusTryAgainLater:
				return "fell behind the output; attach again"
			}
			return "connection lost: " + err.Error()
		}
		if typ == websocket.MessageBinary {
			_, _ = os.Stdout.Write(data)
			continue
		}
		var msg struct {
			Type string
			Code *int
		}
		if json.Unmarshal(data, &msg) == nil && msg.Type == "exit" && msg.Code != nil {
			return fmt.Sprintf("process exited with code %d", *msg.Code)
		}
	}
}
