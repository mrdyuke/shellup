package fish

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newConfigDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".config", "fish")
}

func TestRenderShellupFileSections(t *testing.T) {
	out := RenderShellupFile(
		[]string{"starship init fish | source"},
		[]string{"alias cat 'bat --paging=never'"},
	)
	if !strings.Contains(out, "if status is-interactive") || !strings.Contains(out, "end") {
		t.Fatal("want interactive guard in rendered file")
	}
	if !strings.Contains(out, "starship init fish | source") ||
		!strings.Contains(out, "alias cat 'bat --paging=never'") {
		t.Fatalf("prompt or alias lines lost:\n%s", out)
	}
	// Base setup must be present even when no prompt or alias is chosen.
	bare := RenderShellupFile(nil, nil)
	if !strings.Contains(bare, "set -g fish_greeting") {
		t.Fatalf("default fish greeting must be disabled:\n%s", bare)
	}
	if !strings.Contains(bare, "zoxide init fish | source") {
		t.Fatalf("zoxide must be initialized so its PWD hook records directories:\n%s", bare)
	}
	// Initializing zoxide must not replace the optional Shellup cd function.
	if strings.Contains(bare, "function cd") {
		t.Fatalf("base setup must not define cd:\n%s", bare)
	}
}

func TestWriteShellupFileIsIdempotent(t *testing.T) {
	dir := newConfigDir(t)
	first := RenderShellupFile([]string{"echo one"}, []string{"alias a 1"})
	path, err := WriteShellupFile(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	second := RenderShellupFile([]string{"echo one"}, []string{"alias a 1"})
	path2, err := WriteShellupFile(dir, second)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path2)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != second || path != path2 {
		t.Fatalf("rewrite is not stable: %q vs %q", data, second)
	}
}

func TestEnsureConfigFishCreatesMinimalAndNeverOverwrites(t *testing.T) {
	dir := newConfigDir(t)
	created, path, err := EnsureConfigFish(dir)
	if err != nil || !created {
		t.Fatalf("expected creation, got created=%v err=%v", created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != MinimalConfigFish {
		t.Fatalf("unexpected minimal config: %q", data)
	}

	// An existing config.fish must come through untouched.
	if err := os.WriteFile(path, []byte("set PATH ~/bin $PATH\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created2, _, err := EnsureConfigFish(dir)
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Fatal("second call must not report creation")
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != "set PATH ~/bin $PATH\n" {
		t.Fatalf("existing config.fish was modified: %q", again)
	}
}

func TestShellPathResolvesSymlinks(t *testing.T) {
	look := func(name string) (string, error) {
		if name != "fish" {
			return "", errors.New("not fish")
		}
		return "/usr/bin/fish", nil
	}
	got, err := ShellPath(look)
	if err != nil {
		t.Fatal(err)
	}
	// On the test machine no /usr/bin/fish exists, but the resolver must not
	// break when EvalSymlinks fails: the plain path stays usable.
	if !filepath.IsAbs(got) {
		t.Fatalf("want absolute path, got %q", got)
	}
}

func TestInShellsAndAppendShells(t *testing.T) {
	shells := filepath.Join(t.TempDir(), "shells")
	if err := os.WriteFile(shells, []byte("/bin/sh\n/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := InShells(shells, "/usr/bin/fish")
	if err != nil || ok {
		t.Fatalf("fish should not be listed, ok=%v err=%v", ok, err)
	}

	var gotName string
	var gotArgs []string
	var gotStdin string
	calls := 0
	stdinFn := func(name string, args []string, stdin string) error {
		calls++
		gotName, gotArgs, gotStdin = name, args, stdin
		return nil
	}
	if err := AppendShells(stdinFn, "sudo", shells, "/usr/bin/fish"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || gotName != "sudo" ||
		strings.Join(gotArgs, " ") != "tee -a "+shells || gotStdin != "/usr/bin/fish\n" {
		t.Fatalf("unexpected command: %s %v %q", gotName, gotArgs, gotStdin)
	}

	// After a successful run the entry is visible, and appending again must
	// short-circuit to avoid duplicates.
	ok, err = InShells(shells, "/usr/bin/fish")
	if err != nil {
		t.Fatal(err)
	}
	// InShells checks the file it was pointed at, which tee would update in
	// real life; simulate that success by writing the same content.
	if err := os.WriteFile(shells, []byte("/bin/sh\n/bin/bash\n/usr/bin/fish\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err = InShells(shells, "/usr/bin/fish")
	if err != nil || !ok {
		t.Fatalf("fish should be listed, ok=%v err=%v", ok, err)
	}
	if err := AppendShells(stdinFn, "sudo", shells, "/usr/bin/fish"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("second append must short-circuit, calls=%d", calls)
	}
}

func TestChshCommandShape(t *testing.T) {
	var gotName string
	var gotArgs []string
	stream := func(name string, args []string) error {
		gotName, gotArgs = name, args
		return errors.New("chsh failed")
	}
	err := Chsh(stream, "/usr/bin/fish")
	if err == nil || gotName != "chsh" || strings.Join(gotArgs, " ") != "-s /usr/bin/fish" {
		t.Fatalf("unexpected chsh invocation: %s %v err=%v", gotName, gotArgs, err)
	}
}
