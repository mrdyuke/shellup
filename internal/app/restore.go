package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"shellup/internal/backup"
	"shellup/internal/fish"
	"shellup/internal/ui"
)

// tidePlugin is the Fisher plugin Shellup installs for the tide prompt.
const tidePlugin = "IlanCosman/tide"

// Restore replaces the clean room with a chosen backup: the current fish
// directory is removed, the original fish directory and starship.toml come
// back, the tide plugin is removed, and the previous login shell is restored.
// It returns the summary.
func Restore(d Deps) ([]string, error) {
	out := pickOut(d)
	names, err := backup.ListBackups(d.Home)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "No backups found in ~/.config/shellup-backups.")
		return nil, nil
	}

	items := make([]ui.Item, 0, len(names))
	for i, name := range names {
		desc := fmt.Sprintf("backup %d of %d in \"%s\"", i+1, len(names), backup.BackupRoot(d.Home))
		items = append(items, ui.Item{Label: name, Description: desc})
	}
	idx, err := d.selectItem("Which backup should be restored?", items)
	if err != nil {
		return nil, err
	}
	chosen := names[idx]
	backupDir := filepath.Join(backup.BackupRoot(d.Home), chosen)

	manifest, err := backup.ReadManifest(backupDir)
	if err != nil {
		return nil, err
	}
	// The manifest is shown before the confirmation so the user knows which
	// login shell and Fisher plugins the restore will put back.
	if data, rerr := os.ReadFile(filepath.Join(backupDir, "manifest.json")); rerr == nil {
		fmt.Fprintf(out, "Manifest:\n%s\n", string(data))
	}

	ok, err := d.confirm(
		"Restore backup "+chosen+"?",
		"The current ~/.config/fish (the Shellup clean room) is deleted. The backed-up fish "+
			"directory and starship.toml are restored, the tide plugin is removed, and the "+
			"previous login shell is set again.",
		false)
	if err != nil {
		return nil, err
	}
	if !ok {
		fmt.Fprintln(out, "Restore declined; nothing was changed.")
		return nil, nil
	}

	summary := []string{}
	// The tide plugin lives inside the clean room, so remove it before the
	// original fish directory is copied back. That keeps a tide installation
	// the user already had in the backup untouched.
	removeTidePlugin(d, out, &summary)
	_, count, rerr := backup.Restore(d.Home, backupDir)
	if rerr != nil {
		return nil, rerr
	}
	summary = append([]string{fmt.Sprintf("restored %s (%d files)", chosen, count)}, summary...)
	if err := restoreLoginShell(d, manifest, out, &summary); err != nil {
		PrintSummary(out, summary)
		return summary, err
	}
	PrintSummary(out, summary)
	return summary, nil
}

// removeTidePlugin undoes the tide installation the clean room may have made.
// Fisher itself stays available; only the prompt plugin is removed.
func removeTidePlugin(d Deps, out io.Writer, summary *[]string) {
	if _, err := d.Runner.Probe("fish", "-c", "type -q fisher"); err != nil {
		return
	}
	fmt.Fprintln(out, "Executing: fish -c 'fisher remove IlanCosman/tide'")
	if err := d.Runner.Stream("fish", "-c", "fisher remove IlanCosman/tide"); err != nil {
		fmt.Fprintln(out, "Warning: tide plugin removal failed:", err)
		*summary = append(*summary, "fisher remove IlanCosman/tide failed; remove it manually")
		return
	}
	*summary = append(*summary, "removed fisher plugin "+tidePlugin)
}

// restoreLoginShell sets the login shell recorded in the manifest when it
// differs from the current one. Failures print the exact manual command.
func restoreLoginShell(d Deps, manifest backup.Manifest, out io.Writer, summary *[]string) error {
	if manifest.LoginShell == "" {
		return nil
	}
	if d.loginShell() == manifest.LoginShell {
		*summary = append(*summary, "login shell already "+manifest.LoginShell)
		return nil
	}
	fmt.Fprintln(out, "Executing: chsh -s "+manifest.LoginShell)
	stream := func(name string, args []string) error { return d.Runner.Stream(name, args...) }
	if err := fish.Chsh(stream, manifest.LoginShell); err != nil {
		fmt.Fprintln(out, "chsh failed:", err)
		fmt.Fprintln(out, "Run it manually:")
		fmt.Fprintln(out, "    chsh -s "+manifest.LoginShell)
		return err
	}
	*summary = append(*summary, "login shell restored to "+manifest.LoginShell)
	return nil
}
