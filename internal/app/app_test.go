package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shellup/internal/backup"
	"shellup/internal/ui"
)

// fakeRunner records command invocations and returns canned probe output.
type fakeRunner struct {
	probeOutput map[string]string
	probeErrors map[string]error
	streamErr   map[string]error
	streams     []string
}

func (f *fakeRunner) Probe(name string, args ...string) (string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	if err, ok := f.probeErrors[key]; ok {
		return "", err
	}
	return f.probeOutput[key], nil
}

func (f *fakeRunner) Stream(name string, args ...string) error {
	key := strings.Join(append([]string{name}, args...), " ")
	f.streams = append(f.streams, key)
	return f.streamErr[key]
}

func (f *fakeRunner) StreamStdin(name string, args []string, stdin string) error {
	return f.Stream(name, args...)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

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

func TestPrepareCleanRoomMovesOldConfigAndCreatesFreshDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	cfg := filepath.Join(home, ".config")
	writeFile(t, filepath.Join(cfg, "fish", "config.fish"), "set -gx FOO bar\n")
	writeFile(t, filepath.Join(cfg, "fish", "conf.d", "mine.fish"), "alias ls 'echo mine'\n")
	writeFile(t, filepath.Join(cfg, "starship.toml"), "[character]\n")

	var buf bytes.Buffer
	confirmed := 0
	d := Deps{
		Home: home,
		Out:  &buf,
		Confirm: func(question, explanation string, defaultYes bool) (bool, error) {
			confirmed++
			if !strings.Contains(question, "~/.config/fish") {
				t.Errorf("question must name the directory it deletes, got %q", question)
			}
			return true, nil
		},
	}

	dir, err := prepareCleanRoom(d, &buf, backup.State{LoginShell: "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed != 1 {
		t.Fatalf("deletion must be confirmed exactly once, got %d", confirmed)
	}

	// The old configuration moved into the backup, recursively.
	if got, err := os.ReadFile(filepath.Join(dir, "fish", "conf.d", "mine.fish")); err != nil || string(got) != "alias ls 'echo mine'\n" {
		t.Fatalf("backup missing old config: %v %q", err, got)
	}

	// The clean room is a fresh, empty fish directory with conf.d only.
	fishDir := filepath.Join(cfg, "fish")
	if info, err := os.Stat(fishDir); err != nil || !info.IsDir() {
		t.Fatalf("clean fish directory missing: %v", err)
	}
	if info, err := os.Stat(filepath.Join(fishDir, "conf.d")); err != nil || !info.IsDir() {
		t.Fatalf("clean conf.d missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fishDir, "config.fish")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old config.fish must not be carried into the clean room")
	}
	// starship.toml is only copied, so it stays where it was.
	if _, err := os.Stat(filepath.Join(cfg, "starship.toml")); err != nil {
		t.Fatalf("starship.toml must stay in place: %v", err)
	}
}

func TestPrepareCleanRoomDeclinedLeavesEverything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	configFish := filepath.Join(home, ".config", "fish", "config.fish")
	writeFile(t, configFish, "set -gx FOO bar\n")

	var buf bytes.Buffer
	d := Deps{
		Home:    home,
		Out:     &buf,
		Confirm: func(string, string, bool) (bool, error) { return false, nil },
	}
	dir, err := prepareCleanRoom(d, &buf, backup.State{LoginShell: "/bin/bash"})
	if !errors.Is(err, errNoChange) {
		t.Fatalf("want errNoChange, got %v", err)
	}
	if dir != "" {
		t.Fatalf("declined clean room must not create a backup, got %q", dir)
	}
	if _, err := os.Stat(configFish); err != nil {
		t.Fatalf("declined clean room must not delete anything: %v", err)
	}
	if names, err := backup.ListBackups(home); err != nil || len(names) != 0 {
		t.Fatalf("declined clean room must not create a backup, got %v %v", names, err)
	}
}

func TestRestoreBringsBackFilesAndLoginShell(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	cfg := filepath.Join(home, ".config")
	writeFile(t, filepath.Join(cfg, "fish", "config.fish"), "set -gx EDITOR vim\n")
	writeFile(t, filepath.Join(cfg, "fish", "functions", "gg.fish"), "function gg; git status; end\n")
	writeFile(t, filepath.Join(cfg, "starship.toml"), "[git_branch]\n")

	backupDir, _, err := backup.Create(home, backup.State{LoginShell: "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the clean room left behind by setup.
	writeFile(t, filepath.Join(cfg, "fish", "config.fish"), "# clean room\n")
	writeFile(t, filepath.Join(cfg, "fish", "conf.d", "shellup.fish"), "# managed by Shellup\n")

	var buf bytes.Buffer
	runner := &fakeRunner{probeOutput: map[string]string{"fish -c type -q fisher": ""}}
	d := Deps{
		Home:       home,
		Out:        &buf,
		Runner:     runner,
		LoginShell: func() string { return "/usr/bin/fish" },
		Select:     func(string, []ui.Item) (int, error) { return 0, nil },
		Confirm:    func(string, string, bool) (bool, error) { return true, nil },
	}

	summary, err := Restore(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) == 0 {
		t.Fatal("restore must report what it did")
	}

	// Original files are back and the clean room is gone.
	if got, err := os.ReadFile(filepath.Join(cfg, "fish", "config.fish")); err != nil || string(got) != "set -gx EDITOR vim\n" {
		t.Fatalf("original config.fish not restored: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(cfg, "fish", "conf.d", "shellup.fish")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("clean room files must not survive the restore")
	}
	if got, err := os.ReadFile(filepath.Join(cfg, "starship.toml")); err != nil || string(got) != "[git_branch]\n" {
		t.Fatalf("starship.toml not restored: %v %q", err, got)
	}

	// The login shell and the tide plugin are put back through commands.
	joined := strings.Join(runner.streams, "\n")
	if !strings.Contains(joined, "chsh -s /bin/bash") {
		t.Fatalf("previous login shell was not restored: %v", runner.streams)
	}
	if !strings.Contains(joined, "fisher remove IlanCosman/tide") {
		t.Fatalf("tide plugin was not removed: %v", runner.streams)
	}
	if !strings.Contains(strings.Join(summary, "\n"), "login shell restored to /bin/bash") {
		t.Fatalf("summary must report the shell restore: %v", summary)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "fish", "config.fish")); err != nil {
		t.Fatalf("backup must remain available: %v", err)
	}
}

func TestRestoreKeepsLoginShellWhenUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, _, err := backup.Create(home, backup.State{LoginShell: "/bin/bash"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	runner := &fakeRunner{}
	d := Deps{
		Home:       home,
		Out:        &buf,
		Runner:     runner,
		LoginShell: func() string { return "/bin/bash" },
		Select:     func(string, []ui.Item) (int, error) { return 0, nil },
		Confirm:    func(string, string, bool) (bool, error) { return true, nil },
	}

	if _, err := Restore(d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(runner.streams, "\n"), "chsh") {
		t.Fatalf("unchanged login shell must not run chsh: %v", runner.streams)
	}
}
