package cli

import "testing"

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Encurtador de links em Go\n":       "Encurtador de links em Go",
		`"Novo projeto xyz."`:               "Novo projeto xyz",
		"**Bluetooth headset drops**\nmore": "Bluetooth headset drops",
		"   ":                               "",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
