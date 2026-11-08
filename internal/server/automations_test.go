package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byjuliocesa/orla/internal/automation"
)

func TestAutomationsTurnOnShowLogsAndGo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(automation.Root(home), "hello")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "AUTOMATION.md"), []byte("---\nname: hello\nrun: echo \"hi from $ORLA_SESSION\"; sleep 30\nmetadata:\n  title: Hello\n---\n"), 0o644)
	srv := newServer(t)
	var s struct {
		ID string `json:"id"`
	}
	json.NewDecoder(request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"cwd": t.TempDir()}).Body).Decode(&s)

	type view struct {
		Name      string              `json:"name"`
		Scope     string              `json:"scope"`
		Instances []automation.Status `json:"instances"`
		On        []string            `json:"on"`
	}
	list := func() []view {
		var out struct {
			Automations []view `json:"automations"`
		}
		json.NewDecoder(request(t, srv, "GET", "/v1/automations", testToken, nil).Body).Decode(&out)
		return out.Automations
	}
	if l := list(); len(l) != 1 || l[0].Scope != "session" || len(l[0].Instances) != 0 {
		t.Fatalf("before = %+v", l)
	}
	if res := request(t, srv, "POST", "/v1/automations/hello/start", testToken, nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("a session automation with no session = %d", res.StatusCode)
	}
	if res := request(t, srv, "POST", "/v1/automations/hello/start?session="+s.ID, testToken, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("start = %d", res.StatusCode)
	}
	var lines struct {
		Lines []automation.Line `json:"lines"`
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(30 * time.Millisecond) {
		json.NewDecoder(request(t, srv, "GET", "/v1/automations/hello/logs?session="+s.ID, testToken, nil).Body).Decode(&lines)
		if len(lines.Lines) > 1 {
			break
		}
	}
	if len(lines.Lines) < 2 || !strings.Contains(lines.Lines[1].Text, "hi from "+s.ID) {
		t.Fatalf("logs = %+v", lines.Lines)
	}
	if l := list(); len(l[0].Instances) != 1 || l[0].Instances[0].State != "running" || len(l[0].On) != 1 || l[0].On[0] != s.ID {
		t.Fatalf("running = %+v", l)
	}
	if res := request(t, srv, "POST", "/v1/automations/hello/stop?session="+s.ID, testToken, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("stop = %d", res.StatusCode)
	}
	if l := list(); len(l[0].Instances) != 0 || len(l[0].On) != 0 {
		t.Fatalf("after stop = %+v", l)
	}
	if res := request(t, srv, "DELETE", "/v1/automations/hello", testToken, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("delete = %d", res.StatusCode)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the folder stays")
	}
}
