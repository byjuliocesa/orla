package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoginShellComesFromPasswd(t *testing.T) {
	sh, _ := os.Executable() // any file that exists stands in for a shell
	p := filepath.Join(t.TempDir(), "passwd")
	os.WriteFile(p, []byte("root:x:0:0::/root:/bin/false\nana:x:1000:1000::/home/ana:"+sh+"\nbob:x:1001:1001::/home/bob:/no/such/shell\n"), 0o600)
	if got := loginShell(p, 1000); got != sh {
		t.Errorf("uid 1000 = %q, want %q", got, sh)
	}
	for _, uid := range []int{0, 1001, 1002} {
		if got := loginShell(p, uid); got != "" {
			t.Errorf("uid %d = %q, want none", uid, got)
		}
	}
}
