// Package backup implements Shellup's clean-room model. The user's complete
// fish configuration is moved into a timestamped backup directory before a
// fresh environment is created, and restored later together with the login
// shell and Fisher plugin state recorded in manifest.json. Backups are plain
// files plus the manifest, so they can also be inspected and restored by hand.
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

// ManifestEntry records one top-level item saved in a backup. Action is
// "moved" for the fish directory, which leaves ~/.config, and "copied" for
// starship.toml, which stays in place.
type ManifestEntry struct {
	Rel    string `json:"rel"`
	IsDir  bool   `json:"is_dir"`
	Action string `json:"action"`
}

// State is the shell state that files alone cannot capture. It must be read
// before the fish directory is moved, because Fisher plugins live inside it.
type State struct {
	LoginShell      string
	FisherInstalled bool
	FisherPlugins   []string
}

// Manifest is written to manifest.json inside every backup directory. Besides
// the saved files it records the login shell and the Fisher plugin list, which
// a restore puts back through chsh and fisher.
type Manifest struct {
	CreatedAt       string          `json:"created_at"`
	Entries         []ManifestEntry `json:"entries"`
	LoginShell      string          `json:"login_shell,omitempty"`
	FisherInstalled bool            `json:"fisher_installed,omitempty"`
	FisherPlugins   []string        `json:"fisher_plugins,omitempty"`
}

// BackupRoot returns the absolute path of the backups directory.
func BackupRoot(home string) string {
	return filepath.Join(configDir(home), BackupRootName)
}

// FishDir returns the fish configuration directory that the clean room owns.
func FishDir(home string) string {
	return filepath.Join(configDir(home), "fish")
}

// Create moves ~/.config/fish into a new timestamped backup directory, copies
// ~/.config/starship.toml when it exists, and writes manifest.json with the
// captured shell state. The fish directory is gone from ~/.config afterwards,
// which is what leaves room for the clean room.
func Create(home string, state State) (string, Manifest, error) {
	dir, err := newBackupDir(home)
	if err != nil {
		return "", Manifest{}, err
	}

	m := Manifest{
		CreatedAt:       stampTime().UTC().Format(time.RFC3339),
		LoginShell:      state.LoginShell,
		FisherInstalled: state.FisherInstalled,
		FisherPlugins:   append([]string(nil), state.FisherPlugins...),
	}

	moved, err := moveFish(home, dir)
	if err != nil {
		return dir, m, fmt.Errorf("move fish configuration: %w", err)
	}
	if moved {
		m.Entries = append(m.Entries, ManifestEntry{Rel: "fish", IsDir: true, Action: "moved"})
	}

	starship := filepath.Join(configDir(home), "starship.toml")
	if _, err := os.Lstat(starship); err == nil {
		if err := copyFile(starship, filepath.Join(dir, "starship.toml")); err != nil {
			return dir, m, fmt.Errorf("back up starship.toml: %w", err)
		}
		m.Entries = append(m.Entries, ManifestEntry{Rel: "starship.toml", Action: "copied"})
	} else if !errors.Is(err, os.ErrNotExist) {
		return dir, m, fmt.Errorf("inspect starship.toml: %w", err)
	}

	if err := writeManifest(dir, m); err != nil {
		return dir, m, err
	}
	return dir, m, nil
}

// ReadManifest loads manifest.json from a backup directory.
func ReadManifest(backupDir string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(backupDir, "manifest.json"))
	if err != nil {
		return m, fmt.Errorf("read manifest.json: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("parse manifest.json: %w", err)
	}
	return m, nil
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

// Restore replaces the current fish configuration with the backed-up one and
// copies starship.toml back when the backup contains it. The clean room is
// removed first, even when the backup has no fish directory (the user had no
// fish configuration before setup).
//
// The returned manifest carries the login shell and Fisher plugin list for the
// caller; putting those back needs commands this package does not run.
func Restore(home, backupDir string) (Manifest, int, error) {
	if err := validateBackupPath(home, backupDir); err != nil {
		return Manifest{}, 0, err
	}
	if _, err := os.Stat(backupDir); err != nil {
		return Manifest{}, 0, fmt.Errorf("backup directory not found: %w", err)
	}
	m, err := ReadManifest(backupDir)
	if err != nil {
		return Manifest{}, 0, err
	}

	fishDst := FishDir(home)
	if err := os.RemoveAll(fishDst); err != nil {
		return m, 0, fmt.Errorf("remove current fish configuration: %w", err)
	}

	files := 0
	fishSrc := filepath.Join(backupDir, "fish")
	if info, statErr := os.Lstat(fishSrc); statErr == nil {
		if !info.IsDir() {
			return m, files, fmt.Errorf("backup fish entry is not a directory")
		}
		n, copyErr := copyTree(fishSrc, fishDst)
		files += n
		if copyErr != nil {
			return m, files, fmt.Errorf("restore fish configuration: %w", copyErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return m, files, fmt.Errorf("inspect backup fish directory: %w", statErr)
	}

	starshipSrc := filepath.Join(backupDir, "starship.toml")
	if _, statErr := os.Lstat(starshipSrc); statErr == nil {
		if err := copyFile(starshipSrc, filepath.Join(configDir(home), "starship.toml")); err != nil {
			return m, files, fmt.Errorf("restore starship.toml: %w", err)
		}
		files++
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return m, files, fmt.Errorf("inspect backup starship.toml: %w", statErr)
	}

	return m, files, nil
}

// newBackupDir creates a unique timestamped directory under the backup root.
func newBackupDir(home string) (string, error) {
	stamp := stampTime().Format("20060102-150405")
	dir := filepath.Join(BackupRoot(home), stamp)
	// Two backups inside the same second must not collide, so the first one
	// stays untouched.
	for n := 2; ; n++ {
		if _, err := os.Lstat(dir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return "", err
		}
		dir = filepath.Join(BackupRoot(home), fmt.Sprintf("%s-%d", stamp, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	return dir, nil
}

// moveFish relocates the whole fish configuration directory into the backup.
// os.Rename keeps permissions and symlinks intact on one filesystem; a copy
// followed by removal covers the cross-filesystem case.
func moveFish(home, backupDir string) (bool, error) {
	src := FishDir(home)
	if _, err := os.Lstat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	dst := filepath.Join(backupDir, "fish")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(src, dst); err == nil {
		return true, nil
	}
	if _, err := copyTree(src, dst); err != nil {
		return false, err
	}
	if err := os.RemoveAll(src); err != nil {
		return false, err
	}
	return true, nil
}

// copyTree copies a directory recursively and reports how many files were
// written. Symlinks are recreated rather than followed.
func copyTree(src, dst string) (int, error) {
	files := 0
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		files++
		return nil
	})
	return files, err
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
