// Command shellup is an interactive CLI that sets up a fish-based developer
// environment: installs shell tools, configures the prompt, adds aliases and
// switches the default shell. It never touches existing configuration without
// a backup and explicit user consent.
package main

import (
	"errors"
	"fmt"
	"os"

	"shellup/internal/app"
	"shellup/internal/ui"
)

// version can be overridden at build time with -ldflags.
var version = "dev"

const usage = `shellup - fish developer environment setup

Usage:
  shellup            Interactive setup (default)
  shellup restore    Restore configuration from a previous backup
  shellup version    Print the build version
  shellup help       Show this help

Security notes:
  - The whole ~/.config/fish directory is moved to
    ~/.config/shellup-backups/<timestamp>/fish before the clean room is
    created. ~/.config/starship.toml is copied, and the login shell plus the
    Fisher plugin list are recorded in manifest.json.
  - Every destructive step asks for explicit confirmation in a bubbletea TUI.
  - Shellup-owned fish code lives only in ~/.config/fish/conf.d/shellup.fish.
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}

	deps := app.NewDeps()
	switch cmd {
	case "", "setup":
		if cmd == "setup" && len(args) > 1 {
			fmt.Fprintln(os.Stderr, "unknown argument:", args[1])
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		_, err := app.Run(deps)
		exit(err)
	case "restore":
		_, err := app.Restore(deps)
		exit(err)
	case "version":
		fmt.Printf("shellup %s\n", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

// exit translates a user-aborted dialog into a quiet exit code instead of a
// Go error trace on screen.
func exit(err error) {
	if err == nil {
		os.Exit(0)
	}
	if errors.Is(err, ui.ErrAborted) {
		fmt.Fprintln(os.Stderr, "Aborted; no changes were made.")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
