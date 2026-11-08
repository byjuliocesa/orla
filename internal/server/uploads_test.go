package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestAttachedFilesAreStoredServedAndGivenToARun(t *testing.T) {
	srv := newServer(t)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "../../screen shot.png")
	fw.Write([]byte("not really a png"))
	mw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/v1/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var up Upload
	if res.StatusCode != http.StatusCreated || json.NewDecoder(res.Body).Decode(&up) != nil {
		t.Fatalf("upload = %d", res.StatusCode)
	}
	if up.Name != "screen shot.png" || up.Size != 16 || up.Type != "image/png" || !strings.HasSuffix(up.Path, "/"+up.ID+"/screen shot.png") {
		t.Fatalf("upload = %+v", up)
	}
	if b, _ := os.ReadFile(up.Path); string(b) != "not really a png" {
		t.Fatalf("stored %q", b)
	}

	got := request(t, srv, "GET", "/v1/uploads/"+up.ID, testToken, nil)
	if b, _ := io.ReadAll(got.Body); got.StatusCode != 200 || string(b) != "not really a png" || !strings.Contains(got.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("get = %d %q", got.StatusCode, b)
	}
	if res := request(t, srv, "GET", "/v1/uploads/upl_../../etc", testToken, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("bad id = %d", res.StatusCode)
	}

	var s struct{ ID string }
	created := request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"cwd": t.TempDir()})
	json.NewDecoder(created.Body).Decode(&s)
	if res := request(t, srv, "POST", "/v1/sessions/"+s.ID+"/runs", testToken, map[string]any{"prompt": "look", "attachments": []string{"upl_000000000000000000000000"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown attachment = %d", res.StatusCode)
	}
	if res := request(t, srv, "POST", "/v1/sessions/"+s.ID+"/runs", testToken, map[string]any{"prompt": "look", "attachments": []string{up.ID}}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("run with attachment = %d", res.StatusCode)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd",
		`C:\Users\a\b.txt`: "b.txt",
		".bashrc":          "bashrc",
		"a\x00b.go":        "a_b.go",
		"":                 "file",
		"..":               "file",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
