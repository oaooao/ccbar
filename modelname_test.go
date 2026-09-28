package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeNamesFile points the display-name table at a temp file for one test.
func writeNamesFile(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model-names.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := modelNamesPath
	modelNamesPath = path
	modelNameCache = nil
	t.Cleanup(func() {
		modelNamesPath = old
		modelNameCache = nil
	})
}

func TestDisplayModelNameResolvesCodename(t *testing.T) {
	writeNamesFile(t, `{"crr-a-max-20260902":"Claude Fable 5.1","deepseek-flash":"DeepSeek V4.1 Flash"}`)

	cases := []struct{ in, want string }{
		{"crr-a-max-20260902", "Claude Fable 5.1"},
		{"CRR-A-MAX-20260902", "Claude Fable 5.1"},
		{"deepseek-flash", "DeepSeek V4.1 Flash"},
	}
	for _, c := range cases {
		if got := displayModelName(c.in); got != c.want {
			t.Errorf("displayModelName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDisplayModelNameKeeps1MSuffix(t *testing.T) {
	writeNamesFile(t, `{"crr-a-max-20260902":"Claude Fable 5.1"}`)

	for _, in := range []string{"crr-a-max-20260902[1m]", "CRR-A-MAX-20260902[1M]"} {
		want := "Claude Fable 5.1 (1M)"
		if got := displayModelName(in); got != want {
			t.Errorf("displayModelName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayModelNamePassesThroughUnknown(t *testing.T) {
	writeNamesFile(t, `{"crr-a-max-20260902":"Claude Fable 5.1"}`)

	// Claude Code already renders its own models with real names; anything the
	// table does not know must survive untouched rather than become "Unknown".
	for _, in := range []string{"Opus 4.6", "Opus 4.6 (1M context)", "some-unlisted-model"} {
		if got := displayModelName(in); got != in {
			t.Errorf("displayModelName(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestDisplayModelNameSurvivesMissingTable(t *testing.T) {
	old := modelNamesPath
	modelNamesPath = filepath.Join(t.TempDir(), "does-not-exist.json")
	modelNameCache = nil
	t.Cleanup(func() {
		modelNamesPath = old
		modelNameCache = nil
	})

	if got := displayModelName("crr-a-max-20260902"); got != "crr-a-max-20260902" {
		t.Errorf("with no table, displayModelName = %q, want the codename unchanged", got)
	}
}
