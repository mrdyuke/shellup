package backup

import (
	"encoding/json"
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
		"fish/config.fish":          "set -x EDITOR vim\n",
		"fish/conf.d/alias.fish":    "alias ls 'echo mine'\n",
		"fish/functions/gg.fish":    "function gg; git status; end\n",
		"starship.toml":             "[git_branch]\n",
		"fish/starship_cached.toml": "",
	}
	for rel, body := range files {
		if rel == "fish/starship_cached.toml" {
			continue
		}
		if err := os.WriteFile(filepath.Join(cfg, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreateDetectsAndCopies(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)
	seed(t, home)

	dir, manifest, err := Create(home)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, BackupRootName) {
		t.Fatalf("backup dir %q not under %s", dir, BackupRootName)
	}

	// Everything seeded must be present in the backup, including subfolders.
	for _, rel := range []string{
		"fish/config.fish",
		"fish/conf.d/alias.fish",
		"fish/functions/gg.fish",
		"starship.toml",
		"manifest.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected %s in backup: %v", rel, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got Manifest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
	rels := map[string]bool{}
	for _, e := range got.Entries {
		rels[e.Rel] = true
	}
	if !rels["fish"] || !rels["starship.toml"] {
		t.Fatalf("manifest missing entries: %v", got.Entries)
	}
	if manifest.CreatedAt == "" || len(manifest.Entries) == 0 {
		t.Fatal("manifest struct not populated")
	}
}

func TestCreateWithNothingToBackUp(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)
	if _, _, err := Create(home); err == nil {
		t.Fatal("want error when there is no configuration to back up")
	}
}

func TestRestoreOverwritesAndPreBacksCurrentState(t *testing.T) {
	stampTime = fixedTime
	home := newHome(t)
	seed(t, home)

	backupDir, _, err := Create(home)
	if err != nil {
		t.Fatal(err)
	}

	// User (or Shellup) changes their config after the backup.
	cfg := filepath.Join(home, ".config")
	if err := os.WriteFile(filepath.Join(cfg, "fish", "config.fish"), []byte("set -x EDITOR nano\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(cfg, "starship.toml")); err != nil {
		t.Fatal(err)
	}

	pre, restored, err := Restore(home, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if pre == "" || restored != 4 {
		t.Fatalf("pre=%q restored=%d, want a pre-restore backup and 4 files", pre, restored)
	}

	// The pre-restore copy must contain the modified config.
	preData, err := os.ReadFile(filepath.Join(pre, "fish", "config.fish"))
	if err != nil {
		t.Fatalf("pre-restore backup incomplete: %v", err)
	}
	if !strings.Contains(string(preData), "EDITOR nano") {
		t.Fatalf("pre-restore backup has stale content: %q", preData)
	}

	// Restored values must match the original backup.
	for rel, want := range map[string]string{
		"fish/config.fish":       "set -x EDITOR vim\n",
		"fish/conf.d/alias.fish": "alias ls 'echo mine'\n",
		"starship.toml":          "[git_branch]\n",
	} {
		data, err := os.ReadFile(filepath.Join(cfg, rel))
		if err != nil {
			t.Errorf("restored %s: %v", rel, err)
			continue
		}
		if string(data) != want {
			t.Errorf("restored %s = %q, want %q", rel, data, want)
		}
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
	if _, _, err := Create(home); err != nil {
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
