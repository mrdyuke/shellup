// Package app wires the Shellup flow: environment detection, package
// installation, backup, prompt and alias configuration and default shell
// switching, all through interactive dialogs that never change anything
// without a backup and an explicit yes.
package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"shellup/internal/aliases"
	"shellup/internal/env"
	"shellup/internal/fish"
	"shellup/internal/pkgmgr"
	"shellup/internal/prompt"
	"shellup/internal/ui"
)

// Deps bundles the runtime inputs so tests can drive the flow against a fake
// home directory and wrapped runners.
type Deps struct {
	Home string
	Out  io.Writer
	// LookPath resolves programs; defaults to exec.LookPath.
	LookPath func(string) (string, error)
	// Runner executes commands with the terminal attached.
	Runner pkgmgr.ExecRunner
	// Sudo is the privilege escalation binary used for explicit operations.
	Sudo string
	// Confirm and Select replace the bubbletea dialogs in tests; nil means
	// the real interactive UI.
	Confirm func(question, explanation string, defaultYes bool) (bool, error)
	Select  func(title string, items []ui.Item) (int, error)
	// LoginShell reports the login shell of the user; nil reads the passwd
	// entry and falls back to $SHELL.
	LoginShell func() string
}

// errNoChange marks that the user declined a critical question; the run ends
// without touching anything.
var errNoChange = errors.New("user declined; no changes were made")

// confirm asks a yes/no question through the injected dialog or the real TUI.
func (d Deps) confirm(question, explanation string, defaultYes bool) (bool, error) {
	if d.Confirm != nil {
		return d.Confirm(question, explanation, defaultYes)
	}
	return ui.Confirm(question, explanation, defaultYes)
}

// selectItem shows a single-choice list through the injected dialog or the
// real TUI.
func (d Deps) selectItem(title string, items []ui.Item) (int, error) {
	if d.Select != nil {
		return d.Select(title, items)
	}
	return ui.Select(title, items)
}

// loginShell resolves the login shell recorded for the current user.
func (d Deps) loginShell() string {
	if d.LoginShell != nil {
		return d.LoginShell()
	}
	return currentLoginShell()
}

// currentLoginShell reads the shell field of the current user's passwd entry,
// which is the authoritative login shell, and falls back to $SHELL.
func currentLoginShell() string {
	if data, err := os.ReadFile("/etc/passwd"); err == nil {
		uid := strconv.Itoa(os.Getuid())
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) >= 7 && fields[2] == uid && fields[6] != "" {
				return fields[6]
			}
		}
	}
	return os.Getenv("SHELL")
}

// NewDeps builds the production dependency set for the current user.
func NewDeps() Deps {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to $HOME; os.UserHomeDir rarely fails on Linux.
		home = os.Getenv("HOME")
	}
	sudo := ""
	if p, lerr := exec.LookPath("sudo"); lerr == nil {
		sudo = p
	} else if p, lerr := exec.LookPath("doas"); lerr == nil {
		sudo = p
	}
	return Deps{Home: home, Runner: pkgmgr.Exec{}, Sudo: sudo}
}

// Run executes the full interactive setup flow and returns the final summary
// together with the error state.
func Run(d Deps) ([]string, error) {
	out := pickOut(d)
	home := d.Home

	osInfo, err := env.Detect(withLook(d))
	if err != nil {
		fmt.Fprintln(out, "Error:", err)
		fmt.Fprintln(out, "Shellup supports Arch Linux (pacman), Fedora (dnf) and Debian/Ubuntu (apt) only.")
		return nil, err
	}
	fmt.Fprintf(out, "Detected: distro %q, package manager %q\n", osInfo.Distro, osInfo.PackageManager)

	pm, err := pkgmgr.New(osInfo.PackageManager)
	if err != nil {
		return nil, err
	}

	var aurHelper string
	if osInfo.PackageManager == env.PMPacman {
		helper, herr := ensureAurHelper(d)
		if herr != nil {
			if !errors.Is(herr, errNoChange) {
				return nil, herr
			}
			fmt.Println(out, "Proceeding without an AUR helper; AUR-only packages will be skipped.")
		}
		aurHelper = helper
	}

	// Preflight: fish must be present before any fish configuration is
	// written, so install it early if possible.
	fishPath, ferr := fish.ShellPath(withLook(d))
	if ferr != nil {
		fmt.Fprintln(out, "fish is not installed.")
		ok, cerr := d.confirm(
			"Install fish with "+pm.Name()+"?",
			"Shellup is built around fish as the login shell; without it nothing is configured.",
			true)
		if cerr != nil {
			return nil, cerr
		}
		if !ok {
			fmt.Fprintln(out, "Cannot continue without fish; exiting without changes.")
			return nil, nil
		}
		if err := pm.Install("fish"); err != nil {
			return nil, fmt.Errorf("install fish: %w", err)
		}
		fmt.Fprintln(out, "fish installed.")
		fishPath, err = fish.ShellPath(lookFromShell)
		if err != nil {
			return nil, err
		}
	}

	promptKind, err := askPrompt(d)
	if err != nil {
		return nil, err
	}
	chosenAliases, err := askAliases(d)
	if err != nil {
		return nil, err
	}

	summary := []string{}

	// Required tools; the declarative table drives every distro.
	specs := pkgmgr.Required
	if promptKind == prompt.KindStarship {
		specs = append(specs, pkgmgr.PromptStarship)
	}
	names := pkgmgr.NamesFor(osInfo.PackageManager, specs)
	skipDebianStaleEza(d, osInfo.PackageManager, names, out, &summary)

	if len(names.Missing) > 0 {
		fmt.Fprintf(out, "%s\n", pkgmgr.DescribeMissing(osInfo.Distro, names.Missing))
	}
	if len(names.Installed) > 0 {
		missing, err := pm.Installed(mapValues(names.Installed)...)
		if err != nil {
			return nil, err
		}
		if len(missing) == 0 {
			summary = append(summary, "all required tools already installed")
		} else {
			avail, err := pm.Available(missing...)
			if err != nil {
				return nil, err
			}
			if len(avail) > 0 {
				if osInfo.PackageManager == env.PMApt {
					fmt.Fprintln(out, "Executing: sudo apt-get update")
					if aerr := pm.Refresh(); aerr != nil {
						fmt.Fprintln(out, "Warning: apt-get update failed; package data may be stale.")
					}
				}
				fmt.Fprintf(out, "Executing install via %s: %s\n", pm.Name(), strings.Join(avail, " "))
				if err := pm.Install(avail...); err != nil {
					return nil, fmt.Errorf("install %s: %w", strings.Join(avail, " "), err)
				}
				summary = append(summary, "installed: "+strings.Join(avail, ", "))
			}
			left := without(missing, avail)
			if aur := aurOnly(aurHelper, left, osInfo.PackageManager); len(aur) > 0 {
				fmt.Fprintf(out, "Executing AUR install via %s: %s\n", aurHelper, strings.Join(aur, " "))
				if err := pm.Install(aur...); err != nil {
					return nil, fmt.Errorf("AUR install %s: %w", strings.Join(aur, " "), err)
				}
				summary = append(summary, "installed via AUR helper: "+strings.Join(aur, ", "))
				left = without(left, aur)
			}
			if len(left) > 0 {
				summary = append(summary, "skipped (not in repositories): "+strings.Join(left, ", "))
				fmt.Fprintf(out, "Skipped (not in repositories): %s\n", strings.Join(left, ", "))
			}
		}
	}

	// The clean room replaces the whole fish configuration, so Shellup first
	// records the shell state and moves the old directory into a backup.
	state := snapshotState(d)
	backupDir, err := prepareCleanRoom(d, out, state)
	if err != nil {
		if errors.Is(err, errNoChange) {
			fmt.Fprintln(out, "No changes made; exiting.")
			return nil, nil
		}
		return nil, err
	}
	summary = append(summary, "clean-room backup: "+backupDir)

	// Prompt configuration; every written file shows up in the summary.
	promptLines, err := configurePrompt(d, promptKind, &summary)
	if err != nil {
		return nil, err
	}

	content := fish.RenderShellupFile(promptLines, aliases.RenderStatements(chosenAliases))
	cfgDir := fishHome(home)
	shellupPath, err := fish.WriteShellupFile(cfgDir, content)
	if err != nil {
		return nil, err
	}
	summary = append(summary, "written (replaced in full): "+shellupPath)

	created, configFish, err := fish.EnsureConfigFish(cfgDir)
	if err != nil {
		return nil, err
	}
	if created {
		summary = append(summary, "created (only when missing): "+configFish)
	}

	if err := ensureShellsEntry(d, fishPath, out, &summary); err != nil {
		return nil, err
	}
	if err := switchShell(d, fishPath, out, &summary); err != nil {
		return nil, err
	}

	PrintSummary(out, summary)
	if promptKind == prompt.KindTide {
		fmt.Fprintln(out, prompt.TideUserHint())
	}
	return summary, nil
}

// aurOnly returns the packages that are only installable through the AUR
// helper; on non-Arch setups this list is always empty.
func aurOnly(helper string, pkgs []string, kind string) []string {
	if helper == "" || len(pkgs) == 0 || kind != env.PMPacman {
		return nil
	}
	return pkgs
}

// pickOut returns d.Out or falls back to stdout for terminal feedback.
func pickOut(d Deps) io.Writer {
	if d.Out == nil {
		return os.Stdout
	}
	return d.Out
}

// withLook returns the configured resolution function or exec.LookPath.
func withLook(d Deps) func(string) (string, error) {
	if d.LookPath != nil {
		return d.LookPath
	}
	return exec.LookPath
}

// lookFromShell resolves binaries like the shell does (command -v), which
// keeps working right after a package was installed in the same process,
// unlike exec.LookPath's cache.
var lookFromShell = func(name string) (string, error) {
	cmd := exec.Command("sh", "-c", "command -v "+shQuote(name))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s not found", name)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("%s not found", name)
	}
	return path, nil
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "") + "'"
}

// fishHome resolves the fish configuration directory for a fake or real home.
func fishHome(home string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "fish")
	}
	return filepath.Join(home, ".config", "fish")
}

// mapValues flattens the package-name mapping into a stable list.
func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// without removes already-processed packages from a missing list.
func without(all, done []string) []string {
	doneSet := map[string]bool{}
	for _, v := range done {
		doneSet[v] = true
	}
	var rest []string
	for _, v := range all {
		if !doneSet[v] {
			rest = append(rest, v)
		}
	}
	return rest
}

// PrintSummary renders the final action list; every file write and command
// the run actually performed must appear here.
func PrintSummary(w io.Writer, summary []string) {
	fmt.Fprintln(w, "\nSummary:")
	for _, line := range summary {
		fmt.Fprintf(w, "  - %s\n", line)
	}
}
