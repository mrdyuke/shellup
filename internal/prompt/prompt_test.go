package prompt

import (
	"strings"
	"testing"
)

func TestCustomConfigFileContentVariants(t *testing.T) {
	dark := CustomAnswers{NerdFontIcons: true, GitStatus: true, Theme: ThemeDark, ShowDuration: true}
	light := CustomAnswers{Theme: ThemeLight}
	lightAgain := CustomAnswers{Theme: ThemeLight}

	darkOut, err := CustomConfigFileContent(dark)
	if err != nil {
		t.Fatal(err)
	}
	lightOut, err := CustomConfigFileContent(light)
	if err != nil {
		t.Fatal(err)
	}
	lightAgainOut, err := CustomConfigFileContent(lightAgain)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(darkOut, "function __shellup_prompt_git") ||
		!strings.Contains(darkOut, "function __shellup_prompt_duration") {
		t.Fatal("dark config with git and duration must include both functions")
	}
	if strings.Contains(lightOut, "__shellup_prompt_git") ||
		strings.Contains(lightOut, "__shellup_prompt_duration") {
		t.Fatal("light config without toggles must omit git/duration functions")
	}
	if lightOut != lightAgainOut {
		t.Fatal("generation is not deterministic for repeated answers")
	}
	if !strings.Contains(darkOut, "set -l last_status $status") {
		t.Fatal("prompt must capture status in the first line of fish_prompt")
	}
	if lightOut == darkOut {
		t.Fatal("themes changed nothing")
	}
}

func TestInvalidThemeRejected(t *testing.T) {
	if _, err := CustomConfigFileContent(CustomAnswers{Theme: "matrix"}); err == nil {
		t.Fatal("unknown theme must be rejected")
	}
}

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
	if len(Choices) != 4 {
		t.Fatalf("want 4 prompt choices, got %d", len(Choices))
	}
	for _, c := range Choices {
		if !c.Kind.Valid() || c.Description == "" {
			t.Fatalf("invalid choice entry: %#v", c)
		}
	}
}
