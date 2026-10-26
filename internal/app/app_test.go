package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintSummary(t *testing.T) {
	var b bytes.Buffer
	PrintSummary(&b, []string{"installed: fish", "written: /home/u/.config/fish/conf.d/shellup.fish"})
	if !strings.Contains(b.String(), "Summary:") {
		t.Fatal("summary header missing")
	}
	if !strings.Contains(b.String(), "  - installed: fish") {
		t.Fatalf("summary entry formatting wrong: %q", b.String())
	}
}

func TestFishHomeHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := fishHome("/home/u"); got != "/xdg/fish" {
		t.Fatalf("want /xdg/fish, got %s", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := fishHome("/home/u"); got != "/home/u/.config/fish" {
		t.Fatalf("want /home/u/.config/fish, got %s", got)
	}
}

func TestFishHomeWithRealTempDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := fishHome(home); got != filepath.Join(home, ".config", "fish") {
		t.Fatalf("unexpected fish home: %s", got)
	}
}

func TestWithoutFiltersProcessed(t *testing.T) {
	got := without([]string{"fzf", "eza"}, []string{"fzf"})
	if len(got) != 1 || got[0] != "eza" {
		t.Fatalf("want [eza], got %v", got)
	}
}

func TestMapValues(t *testing.T) {
	got := mapValues(map[string]string{"fish": "fish", "bat": "bat"})
	if len(got) != 2 {
		t.Fatalf("want 2 values, got %v", got)
	}
}

func TestNewDepsResolvesBasics(t *testing.T) {
	deps := NewDeps()
	if deps.Home == "" {
		t.Fatal("NewDeps must resolve home directory")
	}
	if deps.Runner == nil {
		t.Fatal("NewDeps must provide an Exec runner")
	}
}
