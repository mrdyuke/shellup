// Package fish owns everything Shellup writes into the user's fish
// configuration. Its rule: Shellup-managed content lives only in
// conf.d/shellup.fish, existing files are never touched silently, and
// /etc/shells plus chsh handling is explicit and logged to the user first.
package fish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LookPathFunc resolves a program name to an absolute path. Mirrors
// exec.LookPath; injectable in tests.
type LookPathFunc func(string) (string, error)

// StreamFunc runs a command attached to the terminal and a stdin feeder,
// mirroring pkgmgr.ExecRunner. Kept as function type to avoid an import
// cycle-free but heavy dependency in unit tests.
type StreamFunc func(name string, args []string) error
type StreamStdinFunc func(name string, args []string, stdin string) error

// ShellupRelPath is the managed file, relative to the fish config directory:
// ~/.config/fish/conf.d/shellup.fish.
const ShellupRelPath = "conf.d/shellup.fish"

// ShellupFile is the absolute path of the managed file, given the fish
// configuration directory (usually ~/.config/fish).
func ShellupFile(configDir string) string {
	return filepath.Join(configDir, ShellupRelPath)
}

// RenderShellupFile builds the complete content of conf.d/shellup.fish from
// prompt initialization lines and alias snippets. The whole file is
// regenerated on every run, which is what makes the setup idempotent.
func RenderShellupFile(promptLines, aliasLines []string) string {
	var b strings.Builder
	b.WriteString("# shellup.fish is managed by Shellup and rewritten in full on every run.\n")
	b.WriteString("# Do not edit by hand: run shellup again, or place overrides in\n")
	b.WriteString("# a separate file under conf.d/ that sorts after \"shellup\".\n\n")
	b.WriteString("if status is-interactive\n")
	for _, line := range promptLines {
		fmt.Fprintf(&b, "    %s\n", line)
	}
	if len(promptLines) > 0 && len(aliasLines) > 0 {
		b.WriteString("\n")
	}
	for _, line := range aliasLines {
		fmt.Fprintf(&b, "    %s\n", line)
	}
	b.WriteString("end\n")
	return b.String()
}

// WriteShellupFile writes conf.d/shellup.fish in one step. The replace-all
// semantics keep repeated runs stable: content never accumulates.
func WriteShellupFile(configDir, content string) (string, error) {
	path := ShellupFile(configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, fmt.Errorf("create conf.d: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return path, fmt.Errorf("write shellup.fish: %w", err)
	}
	return path, nil
}

// MinimalConfigFish is created only when the user has no config.fish yet.
const MinimalConfigFish = `# Plain small fish configuration created by Shellup.
# Shellup keeps its own settings in conf.d/shellup.fish (loaded automatically).
# Put personal configuration below this line.
`

// EnsureConfigFish creates a minimal ~/.config/fish/config.fish for users who
// do not have one. An existing file is never modified; the report says so.
func EnsureConfigFish(configDir string) (created bool, path string, err error) {
	path = filepath.Join(configDir, "config.fish")
	switch _, statErr := os.Stat(path); {
	case statErr == nil:
		return false, path, nil
	case !errors.Is(statErr, os.ErrNotExist):
		return false, path, statErr
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return false, path, err
	}
	if err := os.WriteFile(path, []byte(MinimalConfigFish), 0o644); err != nil {
		return false, path, err
	}
	return true, path, nil
}

// ShellPath resolves the fish binary to an absolute canonical path, as
// expected as an entry in /etc/shells.
func ShellPath(look LookPathFunc) (string, error) {
	if _, err := look("fish"); err != nil {
		return "", errors.New("fish is not installed; let Shellup install it first")
	}
	path, err := look("fish")
	if err != nil {
		return "", err
	}
	if resolved, linkErr := filepath.EvalSymlinks(path); linkErr == nil {
		path = resolved
	}
	return path, nil
}

// InShells reports whether the fish path is already listed in the shells
// file. Reading /etc/shells may fail; that is reported, not swallowed.
func InShells(shellsPath, fishPath string) (bool, error) {
	data, err := os.ReadFile(shellsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == fishPath {
			return true, nil
		}
	}
	return false, nil
}

// AppendShells adds the fish binary to the shells file using sudo tee -a. The
// caller is responsible for telling the user what will be executed before
// this runs, and for confirming.
func AppendShells(streamStdin StreamStdinFunc, sudo, shellsPath, fishPath string) error {
	if fishPath == "" {
		return errors.New("empty fish path")
	}
	if ok, err := InShells(shellsPath, fishPath); err != nil {
		return fmt.Errorf("read %s: %w", shellsPath, err)
	} else if ok {
		return nil
	}
	// `sudo tee -a` keeps the shell file mode intact without redirect tricks.
	return streamStdin(sudo, []string{"tee", "-a", shellsPath}, fishPath+"\n")
}

// Chsh switches the login shell to fish. It runs chsh so PAM can prompt for
// the password; the caller streams the terminal into it. Failures must be
// reported back with the exact manual command to run.
func Chsh(stream StreamFunc, fishPath string) error {
	return stream("chsh", []string{"-s", fishPath})
}
