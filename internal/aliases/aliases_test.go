package aliases

import (
	"strings"
	"testing"
)

func TestRenderStatementsInCanonicalOrder(t *testing.T) {
	// Answers intentionally out of order; output must still be option order.
	chosen := map[string]bool{"cat-bat": true, "cd-zoxide": true, "ls-eza": true}
	got := RenderStatements(chosen)
	want := strings.Join([]string{
		"zoxide init fish --cmd cd | source",
		"alias cat 'bat --paging=never'",
		"alias ls 'eza --icons'",
	}, "\n")
	if strings.Join(got, "\n") != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestRenderStatementsSkipsDeclined(t *testing.T) {
	chosen := map[string]bool{"cd-zoxide": true}
	got := RenderStatements(chosen)
	if len(got) != 1 || !strings.Contains(got[0], "zoxide") {
		t.Fatalf("declined options must vanish, got %v", got)
	}
}

func TestRenderStatementsAllDeclined(t *testing.T) {
	got := RenderStatements(map[string]bool{"cat-bat": false})
	if len(got) != 0 && len(got) != 1 {
		t.Fatalf("nothing chosen must render empty or single hdr only, got %v", got)
	}
}

func TestByKey(t *testing.T) {
	if _, ok := ByKey("cat-bat"); !ok {
		t.Fatal("cat-bat must exist")
	}
	if _, ok := ByKey("nope"); ok {
		t.Fatal("unknown key must not exist")
	}
	if CatExplanation == "" {
		t.Fatal("cat explanation missing")
	}
}
