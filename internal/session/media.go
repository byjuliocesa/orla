package session

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Pictures a run shows in the timeline come two ways: those a tool returns (a screenshot the
// agent read) and those a run writes to disk (a screenshot a script saved). Both are kept as
// uploads, and the event that reports them carries their ids.

// scan is what a run's search for new pictures has covered.
type scan struct {
	since time.Time
	seen  map[string]bool
}

const (
	maxFound     = 12       // pictures one step can add
	maxImageSize = 15 << 20 // larger is not a screenshot
)

var imageExt = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif"}

// withImages turns a tool result's base64 pictures into kept files, and adds the pictures the
// run wrote to disk since its last step.
func (s *Service) withImages(run, cwd string, data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	kept := []map[string]any{}
	if imgs, ok := m["images"].([]map[string]any); ok {
		for i, img := range imgs {
			b, err := base64.StdEncoding.DecodeString(fmt.Sprint(img["data"]))
			if err != nil {
				continue
			}
			mt := fmt.Sprint(img["media_type"])
			ext := ".png"
			if e, _ := mime.ExtensionsByType(mt); len(e) > 0 {
				ext = e[len(e)-1]
			}
			if k := s.keepImage(fmt.Sprintf("image-%d%s", i+1, ext), mt, b); k != nil {
				kept = append(kept, k)
			}
		}
	}
	for _, path := range s.newImages(run, cwd) {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if k := s.keepImage(filepath.Base(path), imageExt[strings.ToLower(filepath.Ext(path))], b); k != nil {
			k["path"] = path
			kept = append(kept, k)
		}
	}
	if len(kept) == 0 && m["images"] == nil {
		return data
	}
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out["images"] = kept
	return out
}

// keepImage writes one picture to a folder of its own under media, as an upload is kept.
func (s *Service) keepImage(name, mediaType string, b []byte) map[string]any {
	if s.media == "" {
		return nil
	}
	var r [12]byte
	_, _ = rand.Read(r[:])
	id := "upl_" + hex.EncodeToString(r[:])
	dir := filepath.Join(s.media, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.log.Warn("keeping an image", "err", err)
		return nil
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
		s.log.Warn("keeping an image", "err", err)
		return nil
	}
	return map[string]any{"id": id, "name": name, "type": mediaType, "size": len(b)}
}

// newImages are the pictures written since the run's last look, in its folder (three levels
// down) and in the temporary folder (two), where scripts save screenshots. Hidden and
// dependency folders are skipped.
func (s *Service) newImages(run, cwd string) []string {
	s.mu.Lock()
	sc := s.scans[run]
	s.mu.Unlock()
	if sc == nil {
		return nil
	}
	now := time.Now()
	var found []string
	look := func(root string, depth int) {
		if root == "" {
			return
		}
		base := strings.Count(filepath.Clean(root), string(filepath.Separator))
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || len(found) >= maxFound {
				if d != nil && d.IsDir() && p != root {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				n := d.Name()
				if p != root && (strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor" || strings.Count(p, string(filepath.Separator))-base >= depth || (s.media != "" && p == s.media)) {
					return fs.SkipDir
				}
				return nil
			}
			if imageExt[strings.ToLower(filepath.Ext(p))] == "" || sc.seen[p] {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxImageSize || !info.ModTime().After(sc.since) {
				return nil
			}
			found = append(found, p)
			return nil
		})
	}
	look(cwd, 3)
	look(os.TempDir(), 2)
	s.mu.Lock()
	for _, p := range found {
		sc.seen[p] = true
	}
	sc.since = now
	s.mu.Unlock()
	return found
}
