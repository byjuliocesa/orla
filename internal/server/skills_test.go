package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

func TestSessionSkillsTurnOffAndOn(t *testing.T) {
	srv := newServer(t)
	var s struct {
		ID        string   `json:"id"`
		SkillsOff []string `json:"skills_off"`
	}
	json.NewDecoder(request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"cwd": t.TempDir()}).Body).Decode(&s)
	if s.SkillsOff == nil || len(s.SkillsOff) != 0 {
		t.Fatalf("a new session leaves out %v", s.SkillsOff)
	}
	res := request(t, srv, "PATCH", "/v1/sessions/"+s.ID, testToken, map[string]any{"skills_off": []string{"impeccable", "prototype"}})
	json.NewDecoder(res.Body).Decode(&s)
	if res.StatusCode != http.StatusOK || !slices.Equal(s.SkillsOff, []string{"impeccable", "prototype"}) {
		t.Fatalf("after turning off = %d %v", res.StatusCode, s.SkillsOff)
	}
	res = request(t, srv, "PATCH", "/v1/sessions/"+s.ID, testToken, map[string]any{"skills_off": []string{}})
	json.NewDecoder(res.Body).Decode(&s)
	if len(s.SkillsOff) != 0 {
		t.Fatalf("after turning all on = %v", s.SkillsOff)
	}
}
