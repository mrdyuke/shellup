// Package backup creates and restores timestamped backups of the user's
// fish, starship, tide, bat, eza and zoxide configuration under
// ~/.config/shellup-backups. Backups are plain copies plus a manifest.json so
// they can also be inspected and restored by hand.
package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// stampTime is replaced in tests to keep directory names deterministic.
var stampTime = time.Now

// BackupRootName is the directory under ~/.config that holds all backups.
const BackupRootName = "shellup-backups"

// ConfigPath describes one piece of user configuration that Shellup watches,
// expressed relative to ~/.config.
type ConfigPath struct {
	Rel   string // e.g. "fish" or "starship.toml"
	IsDir bool
}

// Watched is the set of configuration locations Shellup backs up before
// touching anything. Entries that do not exist are simply skipped.
var Watched = []ConfigPath{
	{Rel: "fish", IsDir: true},
	{Rel: "starship.toml", IsDir: false},
	{Rel: "tide", IsDir: true},
	{Rel: "bat", IsDir: true},
	{Rel: "eza", IsDir: true},
	{Rel: "zoxide", IsDir: true},
}

// ManifestEntry records one backed-up item at the top level (a watched file
// or directory). During a backup every entry necessarily already existed, so
// Action is "copied"; manifests are informational and restorations walk the
// backup directory itself for full fidelity.
type ManifestEntry struct {
	Rel    string `json:"rel"`
	IsDir  bool   `json:"is_dir"`
	Action string `json:"action"`
}

// Manifest is written to manifest.json inside every backup directory. It
// records both what was copied and, via the watched set, what Shellup may
// overwrite during this run.
type Manifest struct {
	CreatedAt  string          `json:"created_at"`
	PreRestore bool            `json:"pre_restore,omitempty"`
	Entries    []ManifestEntry `json:"entries"`
}

// DetectExisting returns the watched paths that currently exist on disk,
// in the stable order of Watched.
func DetectExisting(home string) []ConfigPath {
	var found []ConfigPath
	for _, cp := range Watched {
		if _, err := os.Lstat(filepath.Join(configDir(home), cp.Rel)); err == nil {
			found = append(found, cp)
		}
	}
	return found
}

// HasExisting reports whether the user already has any watched configuration.
func HasExisting(home string) bool {
	return len(DetectExisting(home)) > 0
}

// BackupRoot returns the absolute path of the backups directory.
func BackupRoot(home string) string {
	return filepath.Join(configDir(home), BackupRootName)
}

// Create copies every existing watched path into a new timestamped backup
// directory and writes manifest.json. It returns the backup directory path.
func Create(home string) (string, Manifest, error) {
	return create(home, false)
}

// CreatePreRestore is Create but tags the manifest as a pre-restore safety
// copy of the state Shellup is about to replace.
func CreatePreRestore(home string) (string, Manifest, error) {
	return create(home, true)
}

func create(home string, preRestore bool) (string, Manifest, error) {
	entries := detectEntries(home)
	if len(entries) == 0 {
		return "", Manifest{}, errors.New("no backed-up configuration found")
	}

	stamp := stampTime().Format("20060102-150405")
	dir := filepath.Join(BackupRoot(home), stamp)
	// Multiple operations inside the same second must not collide: the first
	// backup in a batch must remain untouched by, say, a pre-restore copy.
	for n := 2; ; n++ {
		if _, err := os.Lstat(dir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return "", Manifest{}, err
		}
		dir = filepath.Join(BackupRoot(home), fmt.Sprintf("%s-%d", stamp, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", Manifest{}, fmt.Errorf("create backup dir: %w", err)
	}

	var errs []error
	for i := range entries {
		if err := copyItem(home, dir, entries[i]); err != nil {
			errs = append(errs, err)
		}
	}
	m := Manifest{
		CreatedAt:  stampTime().UTC().Format(time.RFC3339),
		PreRestore: preRestore,
		Entries:    entries,
	}
	if err := writeManifest(dir, m); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return dir, m, fmt.Errorf("backup incomplete: %w", err)
	}
	return dir, m, nil
}

// ListBackups returns backup directory names sorted from oldest to newest.
func ListBackups(home string) ([]string, error) {
	root := BackupRoot(home)
	files, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, f := range files {
		if f.IsDir() {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Restore copies every non-manifest file from backupDir back to its location
// under ~/.config. To keep the operation reversible it first takes a fresh
// pre-restore backup of the current state (when any watched config exists).
// backupDir must live inside the Shellup backups directory.
func Restore(home, backupDir string) (preRestoreDir string, count int, err error) {
	if err := validateBackupPath(home, backupDir); err != nil {
		return "", 0, err
	}
	if _, err := os.Stat(backupDir); err != nil {
		return "", 0, fmt.Errorf("backup directory not found: %w", err)
	}

	preRestoreDir = ""
	if HasExisting(home) {
		preRestoreDir, _, err = CreatePreRestore(home)
		if err != nil {
			return "", 0, fmt.Errorf("pre-restore backup failed: %w", err)
		}
	}

	var errs []error
	walkErr := filepath.WalkDir(backupDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "manifest.json" {
			return nil
		}
		rel, err := filepath.Rel(backupDir, path)
		if err != nil {
			return err
		}
		if err := copyFile(path, filepath.Join(configDir(home), rel)); err != nil {
			return fmt.Errorf("restore %s: %w", rel, err)
		}
		count++
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	if err := errors.Join(errs...); err != nil {
		return preRestoreDir, count, err
	}
	return preRestoreDir, count, nil
}

// validateBackupPath rejects paths outside the backups directory, so users
// cannot accidentally order a restore of an arbitrary tree.
func validateBackupPath(home, backupDir string) error {
	root, err := filepath.Abs(BackupRoot(home))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(backupDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("invalid backup directory: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("invalid backup directory: %s is not inside %s", target, root)
	}
	return nil
}

// detectEntries returns manifest skeleton entries for everything that exists.
func detectEntries(home string) []ManifestEntry {
	var entries []ManifestEntry
	for _, cp := range DetectExisting(home) {
		entries = append(entries, ManifestEntry{Rel: cp.Rel, IsDir: cp.IsDir})
	}
	return entries
}

// copyItem copies one watched path (file or directory tree) into the backup
// directory, preserving the layout relative to ~/.config so a restore can
// walk the backup and put every file back where it came from.
func copyItem(home, backupDir string, entry ManifestEntry) error {
	src := filepath.Join(configDir(home), entry.Rel)
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(configDir(home), path)
		if err != nil {
			return err
		}
		dest := filepath.Join(backupDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return copyFile(path, dest)
	})
}

func writeManifest(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644)
}

// copyFile copies a regular file or a symlink, creating parent directories
// and preserving the permission bits of the source.
func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file type at %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// configDir resolves ~/.config, honoring XDG_CONFIG_HOME when set and
// non-empty, mirroring fish's own config resolution.
func configDir(home string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg
	}
	return filepath.Join(home, ".config")
}
