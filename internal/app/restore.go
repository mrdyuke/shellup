package app

import (
	"fmt"
	"os"
	"path/filepath"

	"shellup/internal/backup"
	"shellup/internal/ui"
)

// Restore offers the list of backups, lets the user pick one, re-backs the
// current state first (so the operation stays reversible) and then copies the
// chosen backup over the current configuration. It returns the summary.
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
	idx, err := ui.Select("Which backup should be restored?", items)
	if err != nil {
		return nil, err
	}
	chosen := names[idx]
	backupDir := filepath.Join(backup.BackupRoot(d.Home), chosen)

	// Manifest description, printed before the confirmation.
	if data, rerr := os.ReadFile(filepath.Join(backupDir, "manifest.json")); rerr == nil {
		fmt.Fprintf(out, "Manifest:\n%s\n", string(data))
	}

	ok, err := ui.Confirm(
		"Restore backup "+chosen+"?",
		"Current configuration is backed up first, so this action remains reversible.",
		false)
	if err != nil {
		return nil, err
	}
	if !ok {
		fmt.Fprintln(out, "Restore declined; nothing was changed.")
		return nil, nil
	}

	pre, count, err := backup.Restore(d.Home, backupDir)
	if err != nil {
		return nil, err
	}
	summary := []string{
		"restored " + chosen + " (" + fmt.Sprintf("%d files", count) + ")",
	}
	if pre != "" {
		summary = append(summary, "pre-restore safety backup: "+pre)
	}
	PrintSummary(out, summary)
	return summary, nil
}
