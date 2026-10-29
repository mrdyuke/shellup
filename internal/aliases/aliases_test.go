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
		CDFallbackFunction(),
		"alias cat 'bat --paging=never'",
		"alias ls 'eza --icons=always'",
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

func TestCDFunctionFallback(t *testing.T) {
	fn := CDFallbackFunction()
	if !strings.HasPrefix(fn, "function cd ") {
		t.Fatalf("snippet must define a cd function, got %q", fn)
	}
	if !strings.Contains(fn, "builtin cd $HOME") ||
		!strings.Contains(fn, "prevd") ||
		!strings.Contains(fn, `test -d "$argv[1]"`) ||
		!strings.Contains(fn, "zoxide query") {
		t.Fatal("fallback function must handle home, dash, existing dirs and zoxide")
	}
	if !strings.Contains(fn, "dirprev") {
		t.Fatal("cd override must maintain the dirprev history so prevd keeps working")
	}
	if !strings.Contains(fn, "cd: no such file or directory") {
		t.Fatal("fallback function must print a clear error")
	}
	if !strings.Contains(fn, "return 1") {
		t.Fatal("fallback function must return a non-zero exit status on failure")
	}
}

func TestLsAliasPassesExplicitIconsValue(t *testing.T) {
	// Recent eza versions treat --icons as an optional value and consume the
	// next argument, so the alias must use the --icons=VALUE form.
	opt, ok := ByKey("ls-eza")
	if !ok {
		t.Fatal("ls-eza must exist")
	}
	if !strings.Contains(opt.Snippet, "--icons=always") {
		t.Fatalf("eza alias must force icons explicitly, got %q", opt.Snippet)
	}
	if strings.Contains(opt.Snippet, "--icons'") {
		t.Fatalf("bare --icons would swallow the next argument, got %q", opt.Snippet)
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
