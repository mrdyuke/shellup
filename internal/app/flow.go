package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"shellup/internal/aliases"
	"shellup/internal/backup"
	"shellup/internal/env"
	"shellup/internal/fish"
	"shellup/internal/pkgmgr"
	"shellup/internal/prompt"
	"shellup/internal/ui"
)

// askPrompt runs the prompt selection dialog. Nothing is written here;
// selection only.
func askPrompt(d Deps) (prompt.Kind, error) {
	items := make([]ui.Item, 0, len(prompt.Choices))
	for _, c := range prompt.Choices {
		items = append(items, ui.Item{Label: string(c.Kind), Description: c.Description})
	}
	idx, err := d.selectItem("Which prompt should Shellup set up?", items)
	if err != nil {
		return prompt.KindNone, err
	}
	return prompt.Choices[idx].Kind, nil
}

// snapshotState captures the login shell and the Fisher plugin list before the
// fish directory is moved, because Fisher plugins live inside that directory.
func snapshotState(d Deps) backup.State {
	state := backup.State{LoginShell: d.loginShell()}
	if _, err := d.Runner.Probe("fish", "-c", "type -q fisher"); err != nil {
		return state
	}
	state.FisherInstalled = true
	out, err := d.Runner.Probe("fish", "-c", "fisher list")
	if err != nil {
		return state
	}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			state.FisherPlugins = append(state.FisherPlugins, line)
		}
	}
	return state
}

// prepareCleanRoom moves the existing fish configuration into a timestamped
// backup and creates an empty fish directory for Shellup to populate. Nothing
// is deleted before the user confirms; declining leaves the system untouched.
func prepareCleanRoom(d Deps, out io.Writer, state backup.State) (string, error) {
	fishDir := fishHome(d.Home)
	if _, err := os.Lstat(fishDir); err == nil {
		ok, cerr := d.confirm(
			"Delete ~/.config/fish and create a clean environment?",
			"The current fish configuration is moved to ~/.config/shellup-backups/<timestamp>/fish "+
				"first, and `shellup restore` puts it back. Nothing from the old configuration is carried over.",
			false)
		if cerr != nil {
			return "", cerr
		}
		if !ok {
			return "", errNoChange
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	dir, manifest, err := backup.Create(d.Home, state)
	if err != nil {
		return "", err
	}
	// Create already moved the directory away; RemoveAll keeps the guarantee
	// even when the move fell back to copying.
	if err := os.RemoveAll(fishDir); err != nil {
		return dir, fmt.Errorf("remove old fish configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(fishDir, "conf.d"), 0o755); err != nil {
		return dir, fmt.Errorf("create clean fish configuration: %w", err)
	}
	fmt.Fprintf(out, "Previous fish configuration saved at %s\n", dir)
	if len(manifest.Entries) == 0 {
		fmt.Fprintln(out, "No existing fish or starship configuration was found; only the shell state was recorded.")
	}
	return dir, nil
}

// askAliases asks every optional alias question in canonical order.
func askAliases(d Deps) (map[string]bool, error) {
	chosen := map[string]bool{}
	for _, o := range aliases.Options {
		ok, err := d.confirm(o.Question, o.Explanation, o.DefaultYes)
		if err != nil {
			return nil, err
		}
		chosen[o.Key] = ok
	}
	return chosen, nil
}

// configurePrompt performs the file and package side of the prompt choice and
// appends honest entries to the summary. It returns the init lines for
// conf.d/shellup.fish.
func configurePrompt(d Deps, kind prompt.Kind, summary *[]string) ([]string, error) {
	out := pickOut(d)
	switch kind {
	case prompt.KindNone:
		*summary = append(*summary, "prompt left untouched as requested")
		return nil, nil

	case prompt.KindStarship:
		if err := ensurePackage(d, pkgmgr.PromptStarship, "starship"); err != nil {
			return nil, err
		}
		tomlPath := filepath.Join(homeRoot(d), "starship.toml")
		if _, err := os.Stat(tomlPath); err == nil {
			*summary = append(*summary, "kept existing: "+tomlPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		} else {
			if err := os.WriteFile(tomlPath, []byte(prompt.MinimalStarshipToml()), 0o644); err != nil {
				return nil, err
			}
			*summary = append(*summary, "created: "+tomlPath)
		}
		return prompt.StarshipInitLines(), nil

	case prompt.KindTide:
		if err := ensureFisher(d, out); err != nil {
			return nil, err
		}
		fmt.Fprintln(out, "Executing: fisher install IlanCosman/tide@v6")
		if err := d.Runner.Stream("fish", "-c", "fisher install IlanCosman/tide@v6"); err != nil {
			return nil, fmt.Errorf("fisher install tide: %w", err)
		}
		*summary = append(*summary, "installed tide v6 via fisher")
		defPath := filepath.Join(fishHome(d.Home), "conf.d", "shellup-tide.fish")
		if err := os.MkdirAll(filepath.Dir(defPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(defPath, []byte(prompt.TideConfigFileContent()), 0o644); err != nil {
			return nil, err
		}
		*summary = append(*summary, "written: "+defPath)
		return nil, nil
	}
	return nil, fmt.Errorf("unknown prompt kind %q", kind)
}

// ensurePackage installs one table-driven package when missing.
func ensurePackage(d Deps, spec pkgmgr.Spec, wantName string) error {
	// The caller must have detected the distro package manager; use the
	// environment again, cheap and side-effect free.
	osInfo, err := env.Detect(withLook(d))
	if err != nil {
		return err
	}
	pm, err := pkgmgr.New(osInfo.PackageManager)
	if err != nil {
		return err
	}
	name := pkgmgr.NamesFor(osInfo.PackageManager, []pkgmgr.Spec{spec}).Installed[spec.Key]
	if name == "" {
		return fmt.Errorf("%s has no package for this distribution (%s)", wantName, osInfo.Distro)
	}
	missing, err := pm.Installed(name)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	return pm.Install(name)
}

// ensureFisher installs fisher through the documented curl bootstrap when it
// is missing. The executed command is printed first.
func ensureFisher(d Deps, out interface{ Write([]byte) (int, error) }) error {
	fmt.Fprintln(out, "Executing: fish -c 'type -q fisher' (check)")
	if err := d.Runner.Stream("fish", "-c", "type -q fisher"); err != nil {
		fmt.Fprintln(out, "fisher not found; installing via curl bootstrap")
		bootstrap := "curl -sL https://raw.githubusercontent.com/jorgebucaran/fisher/main/functions/fisher.fish | source " +
			"&& fisher install jorgebucaran/fisher"
		fmt.Fprintln(out, "Executing: fish -c '"+bootstrap+"'")
		if err := d.Runner.Stream("fish", "-c", bootstrap); err != nil {
			return fmt.Errorf("fisher bootstrap: %w", err)
		}
	}
	return nil
}

// ensureAurHelper looks for yay/paru and, on request, builds yay-bin from the
// AUR. Refusing only disables AUR packages; it never aborts the run.
func ensureAurHelper(d Deps) (string, error) {
	helper, _ := env.FindAurHelper(withLook(d))
	if helper != "" {
		fmt.Fprintf(pickOut(d), "AUR helper found: %s\n", helper)
		return helper, nil
	}
	fmt.Fprintln(pickOut(d), "No AUR helper (yay or paru) found.")
	ok, err := d.confirm(
		"Install yay-bin from the AUR via git clone and makepkg -si?",
		"Shellup will run: git clone https://aur.archlinux.org/yay-bin.git && makepkg -si. It needs the base-devel and git packages.",
		false)
	if err != nil {
		return "", errNoChange
	}
	if !ok {
		fmt.Fprintln(pickOut(d), "Proceeding without an AUR helper; AUR-only packages will be skipped.")
		return "", errNoChange
	}
	if err := installYayBin(d); err != nil {
		return "", err
	}
	if helper, _ := env.FindAurHelper(withLook(d)); helper != "" {
		fmt.Fprintln(pickOut(d), "yay installed.")
		return helper, nil
	}
	return "", errors.New("yay-bin build finished but the helper is not on PATH")
}

// installYayBin clones the yay-bin AUR repository and builds it. Every command
// is announced before running; output streams to the user.
func installYayBin(d Deps) error {
	out := pickOut(d)
	fmt.Fprintln(out, "Executing: git clone https://aur.archlinux.org/yay-bin.git")
	tmp, err := os.MkdirTemp("", "shellup-yay")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	clone := exec.Command("git", "clone", "https://aur.archlinux.org/yay-bin.git", filepath.Join(tmp, "yay-bin"))
	clone.Stdout, clone.Stderr = out, out
	if err := clone.Run(); err != nil {
		return fmt.Errorf("clone yay-bin: %w", err)
	}
	fmt.Fprintln(out, "Executing: makepkg -si (in the clone directory)")
	build := exec.Command("makepkg", "-si")
	build.Dir = filepath.Join(tmp, "yay-bin")
	build.Stdout, build.Stderr, build.Stdin = out, out, os.Stdin
	if err := build.Run(); err != nil {
		return fmt.Errorf("build yay-bin: %w", err)
	}
	return nil
}

// skipDebianStaleEza removes eza from the apt install set when the candidate
// is missing or older than the supported baseline and offers cargo instead,
// exactly as required. It mutates names.Installed in place.
func skipDebianStaleEza(d Deps, kind string, names pkgmgr.SplitNames, out interface{ Write([]byte) (int, error) }, summary *[]string) {
	if kind != env.PMApt {
		return
	}
	pkg, ok := names.Installed["eza"]
	if !ok {
		return
	}
	policy, _ := d.Runner.Probe("apt-cache", "policy", pkg)
	candidate := pkgmgr.ParseAptCandidate(policy)
	if candidate != "" && pkgmgr.VersionAtLeast(candidate, "0.10") {
		return
	}
	if candidate == "" {
		fmt.Fprintln(out, "Warning: eza has no candidate version in the apt repositories.")
	} else {
		fmt.Fprintf(out, "Warning: apt candidate for eza is %s, older than the supported 0.10 baseline.\n", candidate)
	}
	delete(names.Installed, "eza")
	if err := offerEzaViaCargo(d, out); err != nil {
		fmt.Fprintln(out, "Skipping eza:", err)
		*summary = append(*summary, "skipped eza (apt version missing or stale)")
		return
	}
	*summary = append(*summary, "installed eza via cargo")
}

// offerEzaViaCargo proposes `cargo install eza` when cargo is available.
func offerEzaViaCargo(d Deps, out interface{ Write([]byte) (int, error) }) error {
	if _, err := withLook(d)("cargo"); err != nil {
		return errors.New("cargo is not installed, so no fallback for eza")
	}
	ok, err := d.confirm(
		"Install eza via cargo?",
		"Shellup will run: cargo install --locked eza. This builds eza from source and may take a few minutes.",
		false)
	if err != nil {
		return errNoChange
	}
	if !ok {
		return errNoChange
	}
	fmt.Fprintln(out, "Executing: cargo install --locked eza")
	cargo := exec.Command("cargo", "install", "--locked", "eza")
	cargo.Stdout, cargo.Stderr, cargo.Stdin = out, out, os.Stdin
	return cargo.Run()
}

// ensureShellsEntry checks /etc/shells and asks before appending fish.
func ensureShellsEntry(d Deps, fishPath string, out interface{ Write([]byte) (int, error) }, summary *[]string) error {
	has, err := fish.InShells("/etc/shells", fishPath)
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	ok, cerr := d.confirm(
		"Add fish to /etc/shells?",
		"chsh refuses shells that are not listed in /etc/shells. Shellup will run: sudo tee -a /etc/shells.",
		true)
	if cerr != nil {
		return cerr
	}
	if !ok {
		fmt.Fprintln(out, "fish not added to /etc/shells; chsh will reject switching until it is added.")
		*summary = append(*summary, "/etc/shells left unchanged on request")
		return nil
	}
	fmt.Fprintln(out, "Executing: sudo tee -a /etc/shells")
	if err := fish.AppendShells(d.Runner.StreamStdin, d.Sudo, "/etc/shells", fishPath); err != nil {
		return err
	}
	*summary = append(*summary, "added "+fishPath+" to /etc/shells")
	return nil
}

// switchShell runs chsh and, on failure, prints the exact manual command and
// the likely reason.
func switchShell(d Deps, fishPath string, out interface{ Write([]byte) (int, error) }, summary *[]string) error {
	ok, err := d.confirm(
		"Switch the default login shell to fish with chsh?",
		fmt.Sprintf("Shellup will run: chsh -s %s. Your password may be requested by PAM.", fishPath),
		false)
	if err != nil {
		return err
	}
	if !ok {
		*summary = append(*summary, "login shell left unchanged on request")
		return nil
	}
	fmt.Fprintln(out, "Executing: chsh -s "+fishPath)
	chsh := func(name string, args []string) error { return d.Runner.Stream(name, args...) }
	if err := fish.Chsh(chsh, fishPath); err != nil {
		fmt.Fprintln(out, "chsh failed:", err)
		fmt.Fprintln(out, "Run it manually:")
		fmt.Fprintln(out, "    chsh -s "+fishPath)
		if listed, serr := fish.InShells("/etc/shells", fishPath); serr == nil && !listed {
			fmt.Fprintln(out, "Likely reason: fish is not listed in /etc/shells; add it first.")
		}
		return err
	}
	*summary = append(*summary, "login shell switched to "+fishPath)
	return nil
}

// homeRoot resolves ~/.config honor XDG_CONFIG_HOME, mirroring fish.
func homeRoot(d Deps) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg
	}
	return filepath.Join(d.Home, ".config")
}
