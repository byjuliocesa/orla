package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestForkContinuesTheConversationAndLimitsAreKept(t *testing.T) {
	srv := newServer(t)
	var s struct {
		ID            string `json:"id"`
		NativeSession string `json:"native_session"`
		ForkOf        string `json:"fork_of"`
		Title         string `json:"title"`
	}
	created := request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"cwd": t.TempDir()})
	json.NewDecoder(created.Body).Decode(&s)
	if res := request(t, srv, "POST", "/v1/sessions/"+s.ID+"/fork", testToken, nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("fork before any run = %d", res.StatusCode)
	}
	if res := request(t, srv, "POST", "/v1/sessions/"+s.ID+"/runs", testToken, map[string]string{"prompt": "go"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("run = %d", res.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		res := request(t, srv, "GET", "/v1/sessions/"+s.ID, testToken, nil)
		var got struct {
			Session struct {
				NativeSession string `json:"native_session"`
				State         string `json:"state"`
			} `json:"session"`
		}
		json.NewDecoder(res.Body).Decode(&got)
		if got.Session.NativeSession != "" && got.Session.State != "working" {
			s.NativeSession = got.Session.NativeSession
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}

	var detail struct {
		Runs []struct{ ID string } `json:"runs"`
	}
	json.NewDecoder(request(t, srv, "GET", "/v1/sessions/"+s.ID, testToken, nil).Body).Decode(&detail)
	if res := request(t, srv, "POST", "/v1/sessions/"+s.ID+"/fork", testToken, map[string]string{"run": "run_nope"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("fork at an unknown run = %d", res.StatusCode)
	}
	// From the latest run's answer is from the end: no transcript needed.
	res := request(t, srv, "POST", "/v1/sessions/"+s.ID+"/fork", testToken, map[string]string{"run": detail.Runs[len(detail.Runs)-1].ID})
	var f struct {
		ID     string `json:"id"`
		ForkOf string `json:"fork_of"`
	}
	json.NewDecoder(res.Body).Decode(&f)
	if res.StatusCode != http.StatusCreated || f.ID == s.ID || f.ForkOf != s.NativeSession {
		t.Fatalf("fork = %d %+v, want fork_of %q", res.StatusCode, f, s.NativeSession)
	}

	lim := request(t, srv, "GET", "/v1/limits", testToken, nil)
	var l struct {
		Limits map[string]struct {
			Used     float64 `json:"used"`
			ResetsAt int64   `json:"resets_at"`
		} `json:"limits"`
	}
	json.NewDecoder(lim.Body).Decode(&l)
	if w, ok := l.Limits["five_hour"]; !ok || w.ResetsAt == 0 {
		t.Fatalf("limits = %+v", l)
	}
}
