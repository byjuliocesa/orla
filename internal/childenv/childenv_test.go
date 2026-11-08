package childenv

import (
	"slices"
	"testing"
)

func TestCleanDropsOnlySessionMarkers(t *testing.T) {
	got := Clean([]string{"PATH=/bin", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDECODE=1", "CLAUDE_EFFORT=max", "CLAUDE_CODE_USE_BEDROCK=1", "HOME=/h"})
	want := []string{"PATH=/bin", "CLAUDE_CODE_USE_BEDROCK=1", "HOME=/h"}
	if !slices.Equal(got, want) {
		t.Fatalf("Clean = %v, want %v", got, want)
	}
}
