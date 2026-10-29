package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newHome builds a fake home directory with a ~/.config inside.
func newHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func seed(t *testing.T, home string) {
	t.Helper()
	cfg := filepath.Join(home, ".config")
	for _, rel := range []string{
		"fish/conf.d",
		"fish/functions",
	} {
		if err := os.MkdirAll(filepath.Join(cfg, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"fish/config.fish":       "set -x EDITOR vim\n",
		"fish/conf.d/alias.fish": "alias ls 'echo mine'\n",
		"fish/functions/gg.fish": "function gg; git status; end\n",
		"starship.toml":          "[git_branch]\n",
	}
	for rel, body := range files {
		writeFile(t, filepath.Join(cfg, rel), body)
	}
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestCreateMovesFishIntoBackup(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)
	seed(t, home)

	dir, manifest, err := Create(home, State{
		LoginShell:      "/bin/bash",
		FisherInstalled: true,
		FisherPlugins:   []string{"jorgebucaran/fisher"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, BackupRootName) {
		t.Fatalf("backup dir %q not under %s", dir, BackupRootName)
	}

	// The whole fish directory is moved away, recursively intact.
	if _, err := os.Stat(filepath.Join(home, ".config", "fish")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fish directory must be moved away, stat err = %v", err)
	}
	for rel, want := range map[string]string{
		"fish/config.fish":       "set -x EDITOR vim\n",
		"fish/conf.d/alias.fish": "alias ls 'echo mine'\n",
		"fish/functions/gg.fish": "function gg; git status; end\n",
	} {
		if got := readFile(t, filepath.Join(dir, rel)); got != want {
			t.Errorf("backup %s = %q, want %q", rel, got, want)
		}
	}

	// starship.toml is saved and stays in place.
	if got := readFile(t, filepath.Join(dir, "starship.toml")); got != "[git_branch]\n" {
		t.Errorf("starship.toml backup = %q", got)
	}
	if got := readFile(t, filepath.Join(home, ".config", "starship.toml")); got != "[git_branch]\n" {
		t.Errorf("starship.toml must remain in place, got %q", got)
	}

	// The shell state that files cannot carry lives in the manifest.
	if manifest.LoginShell != "/bin/bash" || !manifest.FisherInstalled {
		t.Fatalf("manifest state = %+v", manifest)
	}
	if len(manifest.FisherPlugins) != 1 || manifest.FisherPlugins[0] != "jorgebucaran/fisher" {
		t.Fatalf("fisher plugins = %v", manifest.FisherPlugins)
	}

	var saved Manifest
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "manifest.json"))), &saved); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
	if saved.LoginShell != "/bin/bash" {
		t.Errorf("saved login shell = %q", saved.LoginShell)
	}
	actions := map[string]string{}
	for _, e := range saved.Entries {
		actions[e.Rel] = e.Action
	}
	if actions["fish"] != "moved" || actions["starship.toml"] != "copied" {
		t.Fatalf("manifest actions = %v", actions)
	}
}

func TestCreateWithoutExistingConfiguration(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)

	dir, manifest, err := Create(home, State{LoginShell: "/bin/zsh"})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 0 {
		t.Fatalf("nothing should have been backed up, got %v", manifest.Entries)
	}
	if manifest.LoginShell != "/bin/zsh" {
		t.Fatalf("login shell = %q", manifest.LoginShell)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatalf("metadata-only backup must still write a manifest: %v", err)
	}
}

func TestRestoreBringsBackFishAndStarship(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)
	seed(t, home)

	backupDir, _, err := Create(home, State{LoginShell: "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}

	// Build the clean room that setup leaves behind, and change starship.toml
	// afterwards to prove the backup wins.
	cfg := filepath.Join(home, ".config")
	writeFile(t, filepath.Join(cfg, "fish", "conf.d", "shellup.fish"), "# managed by Shellup\n")
	writeFile(t, filepath.Join(cfg, "fish", "config.fish"), "# clean room config\n")
	writeFile(t, filepath.Join(cfg, "starship.toml"), "changed = true\n")

	manifest, count, err := Restore(home, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("restored %d files, want 4", count)
	}
	if manifest.LoginShell != "/bin/bash" {
		t.Fatalf("manifest login shell = %q", manifest.LoginShell)
	}

	if got := readFile(t, filepath.Join(cfg, "fish", "config.fish")); got != "set -x EDITOR vim\n" {
		t.Errorf("config.fish = %q", got)
	}
	if got := readFile(t, filepath.Join(cfg, "fish", "conf.d", "alias.fish")); got != "alias ls 'echo mine'\n" {
		t.Errorf("nested config = %q", got)
	}
	if _, err := os.Stat(filepath.Join(cfg, "fish", "conf.d", "shellup.fish")); !errors.Is(err, os.ErrNotExist) {
		t.Error("clean room files must not survive a restore")
	}
	if got := readFile(t, filepath.Join(cfg, "starship.toml")); got != "[git_branch]\n" {
		t.Errorf("starship.toml = %q", got)
	}
	// The backup stays usable for another restore.
	if got := readFile(t, filepath.Join(backupDir, "fish", "config.fish")); got != "set -x EDITOR vim\n" {
		t.Errorf("backup was consumed by the restore: %q", got)
	}
}

func TestRestoreRemovesCleanRoomWhenBackupHasNoFish(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)

	backupDir, _, err := Create(home, State{LoginShell: "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".config", "fish", "conf.d", "shellup.fish"), "# managed\n")

	if _, count, err := Restore(home, backupDir); err != nil {
		t.Fatal(err)
	} else if count != 0 {
		t.Fatalf("restored %d files, want 0", count)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "fish")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clean room must be removed, stat err = %v", err)
	}
}

func TestRestoreRejectsPathOutsideBackups(t *testing.T) {
	home := newHome(t)
	victim := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Restore(home, victim); err == nil {
		t.Fatal("want error for restore of a directory outside the backups root")
	}
}

func TestListBackups(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)
	if names, err := ListBackups(home); err != nil || len(names) != 0 {
		t.Fatalf("want no backups, got %v %v", names, err)
	}
	seed(t, home)
	if _, _, err := Create(home, State{LoginShell: "/bin/bash"}); err != nil {
		t.Fatal(err)
	}
	names, err := ListBackups(home)
	if err != nil || len(names) != 1 {
		t.Fatalf("want one backup, got %v %v", names, err)
	}
}

// fixedTime is returned by the stamped-time override so backup directory
// names are deterministic in tests.
func fixedTime() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
