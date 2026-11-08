package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPrefsAreKeptAndReturned(t *testing.T) {
	srv := newServer(t)
	var got struct {
		Value json.RawMessage `json:"value"`
	}
	res := request(t, srv, "GET", "/v1/prefs/skills.layout", testToken, nil)
	json.NewDecoder(res.Body).Decode(&got)
	if res.StatusCode != http.StatusOK || string(got.Value) != "null" {
		t.Fatalf("before any = %d %s", res.StatusCode, got.Value)
	}
	layout := map[string]any{"groups": []any{map[string]any{"id": "design", "title": "Design", "skills": []string{"impeccable"}, "collapsed": true}}}
	if res := request(t, srv, "PUT", "/v1/prefs/skills.layout", testToken, layout); res.StatusCode != http.StatusOK {
		t.Fatalf("put = %d", res.StatusCode)
	}
	res = request(t, srv, "GET", "/v1/prefs/skills.layout", testToken, nil)
	json.NewDecoder(res.Body).Decode(&got)
	var back struct {
		Groups []struct {
			ID        string   `json:"id"`
			Skills    []string `json:"skills"`
			Collapsed bool     `json:"collapsed"`
		} `json:"groups"`
	}
	json.Unmarshal(got.Value, &back)
	if len(back.Groups) != 1 || back.Groups[0].ID != "design" || !back.Groups[0].Collapsed || back.Groups[0].Skills[0] != "impeccable" {
		t.Fatalf("kept = %s", got.Value)
	}
	if res := request(t, srv, "PUT", "/v1/prefs/Bad Key", testToken, layout); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad key = %d", res.StatusCode)
	}
	if res := request(t, srv, "GET", "/v1/prefs/skills.layout", "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a token = %d", res.StatusCode)
	}
}
