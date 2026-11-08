package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestArgsCarryEffortFoldersAndFork(t *testing.T) {
	got := strings.Join(Claude{}.args(Spec{
		Prompt: "go", Resume: "abc", Fork: true, ResumeAt: "m1", Effort: "max", Settings: SkillSettings([]string{"x"}), AddDirs: []string{"/u/1", "/u/2"}, System: "about flows",
	}), " ")
	for _, want := range []string{"--resume abc --fork-session --resume-session-at m1", "--effort max", `--settings {"skillOverrides":{"x":"off"}}`, "--add-dir /u/1", "--add-dir /u/2", "--append-system-prompt about flows"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q lack %q", got, want)
		}
	}
	if slices.Contains(Claude{}.args(Spec{Prompt: "go", Fork: true}), "--fork-session") {
		t.Error("--fork-session without --resume")
	}
}
