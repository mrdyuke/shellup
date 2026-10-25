// Package pkgmgr wraps the supported package managers (pacman, apt, dnf) and
// AUR helpers behind a narrow interface so the rest of Shellup can install
// packages without caring about distribution details. All long-running
// commands stream their output to the user so sudo password prompts work.
package pkgmgr

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"shellup/internal/env"
)

// ExecRunner abstracts command execution. Probe captures output and is meant
// for quick checks; Stream attaches the parent terminal so the user can see
// progress and answer prompts; StreamStdin feeds a fixed stdin payload, which
// is how Shellup appends to /etc/shells through sudo tee.
type ExecRunner interface {
	Probe(name string, args ...string) (string, error)
	Stream(name string, args ...string) error
	StreamStdin(name string, args []string, stdin string) error
}

// Exec is the real, terminal-attached implementation of ExecRunner.
type Exec struct{}

// Probe runs a command and captures its stdout, discarding stderr.
func (Exec) Probe(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

// Stream runs a command with stdin, stdout and stderr wired to the terminal.
func (Exec) Stream(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = osStdin(), osStdout(), osStderr()
	return cmd.Run()
}

// StreamStdin runs a command with stdout/stderr attached and stdin fed from a
// fixed string.
func (Exec) StreamStdin(name string, args []string, stdin string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = osStdout(), osStderr()
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// PackageManager installs packages and inspects their state.
type PackageManager interface {
	// Name is a human-readable identifier such as "pacman" or "apt".
	Name() string
	// Installed filters pkgs down to those NOT currently installed.
	Installed(pkgs ...string) ([]string, error)
	// Available filters pkgs down to those known to the configured
	// repositories. AUR wrappers report everything as available because the
	// helper itself performs the final lookup.
	Available(pkgs ...string) ([]string, error)
	// Install installs the given packages, streaming output to the user.
	Install(pkgs ...string) error
	// Refresh updates local package metadata; it is a no-op for managers
	// that do it implicitly.
	Refresh() error
}

// New resolves a PackageManager for one of the env.PM* constants.
func New(kind string) (PackageManager, error) {
	sudoer, err := lookSudo()
	if err != nil {
		return nil, err
	}
	switch kind {
	case env.PMPacman:
		return pacman{runner: Exec{}, sudo: sudoer}, nil
	case env.PMApt:
		return apt{runner: Exec{}, sudo: sudoer}, nil
	case env.PMDnf:
		return dnf{runner: Exec{}, sudo: sudoer}, nil
	default:
		return nil, fmt.Errorf("unsupported package manager %q", kind)
	}
}

// NewWithRunner is New with an injected runner, for tests.
func NewWithRunner(kind string, runner ExecRunner, sudo string) (PackageManager, error) {
	switch kind {
	case env.PMPacman:
		return pacman{runner: runner, sudo: sudo}, nil
	case env.PMApt:
		return apt{runner: runner, sudo: sudo}, nil
	case env.PMDnf:
		return dnf{runner: runner, sudo: sudo}, nil
	default:
		return nil, fmt.Errorf("unsupported package manager %q", kind)
	}
}

// NewAUR wraps an AUR helper (yay or paru) as a PackageManager. The helper
// must already exist on PATH; detecting and offering to build one is handled
// by the caller.
func NewAUR(helper string) PackageManager {
	return aurHelper{runner: Exec{}, helper: helper}
}

func lookSudo() (string, error) {
	if _, err := exec.LookPath("sudo"); err == nil {
		return "sudo", nil
	}
	if _, err := exec.LookPath("doas"); err == nil {
		return "doas", nil
	}
	return "", fmt.Errorf(
		"neither sudo nor doas is available; Shellup needs a privilege escalation tool to install packages")
}

// pacman uses sudo pacman -S --needed, exactly as required: --needed skips
// packages that are already installed and up to date.
type pacman struct {
	runner ExecRunner
	sudo   string
}

func (p pacman) Name() string { return env.PMPacman }

func (p pacman) Installed(pkgs ...string) ([]string, error) {
	var missing []string
	for _, pkg := range pkgs {
		if _, err := p.runner.Probe("pacman", "-Q", pkg); err != nil {
			missing = append(missing, pkg)
		}
	}
	return missing, nil
}

func (p pacman) Available(pkgs ...string) ([]string, error) {
	var found []string
	for _, pkg := range pkgs {
		if _, err := p.runner.Probe("pacman", "-Si", pkg); err == nil {
			found = append(found, pkg)
		}
	}
	return found, nil
}

func (p pacman) Install(pkgs ...string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{p.sudo, "pacman", "-S", "--needed"}, pkgs...)
	return p.runner.Stream(args[0], args[1:]...)
}

func (p pacman) Refresh() error { return nil }

// apt uses dpkg-query, apt-cache and sudo apt-get. The update step keeps
// candidate information honest on systems that have not run update lately.
type apt struct {
	runner ExecRunner
	sudo   string
}

func (a apt) Name() string { return env.PMApt }

func (a apt) Installed(pkgs ...string) ([]string, error) {
	var missing []string
	for _, pkg := range pkgs {
		out, err := a.runner.Probe("dpkg-query", "-W", "-f=${db:Status-Status}", pkg)
		if err != nil || strings.TrimSpace(out) != "installed" {
			missing = append(missing, pkg)
		}
	}
	return missing, nil
}

func (a apt) Available(pkgs ...string) ([]string, error) {
	var found []string
	for _, pkg := range pkgs {
		out, err := a.runner.Probe("apt-cache", "show", pkg)
		if err == nil && strings.TrimSpace(out) != "" {
			found = append(found, pkg)
		}
	}
	return found, nil
}

func (a apt) Install(pkgs ...string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{a.sudo, "apt-get", "install", "-y"}, pkgs...)
	return a.runner.Stream(args[0], args[1:]...)
}

func (a apt) Refresh() error {
	return a.runner.Stream(a.sudo, "apt-get", "update")
}

type dnf struct {
	runner ExecRunner
	sudo   string
}

func (d dnf) Name() string { return env.PMDnf }

func (d dnf) Installed(pkgs ...string) ([]string, error) {
	var missing []string
	for _, pkg := range pkgs {
		if _, err := d.runner.Probe("rpm", "-q", pkg); err != nil {
			missing = append(missing, pkg)
		}
	}
	return missing, nil
}

func (d dnf) Available(pkgs ...string) ([]string, error) {
	var found []string
	for _, pkg := range pkgs {
		out, err := d.runner.Probe("dnf", "info", pkg)
		if err == nil && strings.TrimSpace(out) != "" {
			found = append(found, pkg)
		}
	}
	return found, nil
}

func (d dnf) Install(pkgs ...string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{d.sudo, "dnf", "install", "-y"}, pkgs...)
	return d.runner.Stream(args[0], args[1:]...)
}

func (d dnf) Refresh() error { return nil }

// aurHelper adapts a yay/paru-style helper. Install delegates -S --needed to
// the helper, which resolves AUR as well as official repos.
type aurHelper struct {
	runner ExecRunner
	helper string
}

func (a aurHelper) Name() string { return "aur (" + a.helper + ")" }

func (a aurHelper) Installed(pkgs ...string) ([]string, error) {
	var missing []string
	for _, pkg := range pkgs {
		if _, err := a.runner.Probe("pacman", "-Q", pkg); err != nil {
			missing = append(missing, pkg)
		}
	}
	return missing, nil
}

func (a aurHelper) Available(pkgs ...string) ([]string, error) {
	return pkgs, nil
}

func (a aurHelper) Install(pkgs ...string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{a.helper, "-S", "--needed"}, pkgs...)
	return a.runner.Stream(args[0], args[1:]...)
}

func (a aurHelper) Refresh() error { return nil }

// SplitNames maps logical tool keys onto distro package names. It reports the
// keys that have no mapping for the distro so the caller can print an honest
// summary instead of guessing.
type SplitNames struct {
	Installed map[string]string // key -> package name, in spec order
	Missing   []string          // keys with no package for this distro
}

// NamesFor resolves specs against a distro kind ("pacman", "apt", "dnf").
func NamesFor(kind string, specs []Spec) SplitNames {
	res := SplitNames{Installed: map[string]string{}}
	for _, spec := range specs {
		var name string
		switch kind {
		case env.PMPacman:
			name = spec.Arch
		case env.PMApt:
			name = spec.Debian
		case env.PMDnf:
			name = spec.Fedora
		}
		if name == "" {
			res.Missing = append(res.Missing, spec.Key)
			continue
		}
		res.Installed[spec.Key] = name
	}
	return res
}

// SortedKeys returns the keys of a NamesFor result in spec-kit order.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
