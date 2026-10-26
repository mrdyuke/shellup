package prompt

import (
	"strings"
	"testing"
)

func TestStarshipPieces(t *testing.T) {
	lines := StarshipInitLines()
	if lines[0] != "starship init fish | source" {
		t.Fatalf("unexpected starship init: %q", lines[0])
	}
	toml := MinimalStarshipToml()
	if !strings.Contains(toml, "[character]") {
		t.Fatal("minimal toml must include a character section")
	}
}

func TestTideDefaultsGuardedOnce(t *testing.T) {
	out := TideConfigFileContent()
	if !strings.Contains(out, "if not set -q tide_prompt_pad_items") {
		t.Fatal("tide defaults must be guarded so user configuration wins")
	}
	if !strings.Contains(out, "tide configure") {
		t.Fatal("tide file must tell the user how to run the configurator")
	}
	if TideUserHint() == "" {
		t.Fatal("tide hint empty")
	}
}

func TestChoicesCoverage(t *testing.T) {
	if len(Choices) != 3 {
		t.Fatalf("want 3 prompt choices, got %d", len(Choices))
	}
	seen := map[Kind]bool{}
	for _, c := range Choices {
		if !c.Kind.Valid() || c.Description == "" {
			t.Fatalf("invalid choice entry: %#v", c)
		}
		if seen[c.Kind] {
			t.Fatalf("duplicate choice %q", c.Kind)
		}
		seen[c.Kind] = true
	}
	if !seen[KindStarship] || !seen[KindTide] || !seen[KindNone] {
		t.Fatalf("expected starship, tide and none choices, got %v", Choices)
	}
}
