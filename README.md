# Shellup

Shellup is an interactive CLI tool that sets up a fish-based developer
environment: it installs a set of modern shell utilities, configures the
prompt, offers optional aliases and switches the default login shell.

Shellup is safe by default. Existing configuration is never replaced without a
timestamped backup and an explicit confirmation in a bubbletea dialog.

## What it does

- Detects the distribution and the package manager: `pacman` (Arch), `apt`
  (Debian/Ubuntu) or `dnf` (Fedora). On an unsupported system Shellup prints a
  clear error and exits without changes.
- On Arch, looks for an AUR helper (`yay`, then `paru`). If none is found, it
  offers to build `yay-bin` via `git clone https://aur.archlinux.org/yay-bin.git`
  followed by `makepkg -si`, after explicit confirmation.
- Installs the required tools: `fish`, `fzf`, `eza`, `bat`, `zoxide`, `btop`,
  plus `starship` when you choose starship as the prompt. All calls announce
  the exact command they are about to run.
- Configures the prompt from one of four interactive choices:
  `starship`, `tide` (installed through fisher), a deterministic `custom`
  prompt generated from four short questions, or `none` (leave the prompt
  untouched).
- Offers optional aliases as separate yes/no questions: `cd` to smartcd via
  zoxide, `cat` to `bat --paging=never`, `ls` to `eza --icons`.
- Writes everything it owns into a single managed file,
  `~/.config/fish/conf.d/shellup.fish`, regenerated in full on every run.
  Your `config.fish` and any other `conf.d/` files are never modified.
- Adds fish to `/etc/shells` (asked first, done via `sudo tee -a`) and offers
  to switch the login shell with `chsh -s /path/to/fish`.
- Prints a summary at the end listing every file written, every package
  installed and everything that was skipped, with reasons.

## Supported distributions and limitations

| Distro        | Package manager | Notes |
| ------------- | --------------- | ----- |
| Arch Linux    | `pacman` + AUR  | Fully supported; packages missing from the official repos go through the AUR helper. |
| Debian/Ubuntu | `apt`           | Fully supported, with the `eza` caveat described below. |
| Fedora        | `dnf`           | Fully supported; all required tools are in the official repositories. |

- `sudo` (or `doas`) must be available for package installation, `/etc/shells`
  edits and `chsh`; Shellup uses it only for those specific operations.
- On systems with none of pacman/apt/dnf (macOS, NixOS, Windows), Shellup exits
  with a clear message. macOS and Windows are explicitly out of scope.

## Backups and restore

Before any configuration is replaced, Shellup copies the existing state into

```
~/.config/shellup-backups/<timestamp>/
```

Backed-up locations (only if they exist):

- `~/.config/fish/` including `conf.d/`, `functions/` and `completions/`
- `~/.config/starship.toml`
- `~/.config/tide/`
- `~/.config/bat/`, `~/.config/eza/`, `~/.config/zoxide/`

Each backup directory contains a `manifest.json` listing every backed-up item
and whether it is a file or a directory. Backups are plain copies; they can
also be restored by hand.

Restore command:

```
shellup restore
```

It lists the available backups, shows the selected manifest, and after an
explicit confirmation copies the chosen backup over the current configuration.
To stay reversible, Shellup first takes a fresh safety backup of the current
state (reported in the summary, directory name suffixed `-2`, `-3`, ... when
several backups are created within the same second).

## How the default shell switch works

`chsh -s $(command -v fish)` (using the resolved absolute fish path) changes
the login shell recorded in `/etc/passwd`. Debian-family systems and other
PAM-based shells require interactive password confirmation, so the command
streams your terminal directly. Shellup asks before running it.

Two conditions matter:

1. fish must be installed. If it is not, Shellup offers to install it first.
2. fish must be listed in `/etc/shells`. `chsh` refuses shells that are not
   listed there, to keep untrusted binaries from becoming login shells. If
   fish is missing from the list, Shellup offers to append it with
   `sudo tee -a /etc/shells`; if you decline (or if `chsh` fails for any other
   reason), Shellup prints the exact manual command to run.

## Known limitations

- AUR builds (including the offered `yay-bin` bootstrap) run `makepkg -si`.
  Flags such as `--cleanbuild` or makepkg-specific build environments are not
  passed through; if a build fails, inspect the build directory and rerun
  manually.
- On older Debian/Ubuntu releases the `eza` package may be missing or older
  than version 0.10. Shellup warns, drops `eza` from the apt install set and
  offers `cargo install --locked eza` when cargo is available; otherwise it
  skips `eza` with an explicit message.
- The `tide` prompt is installed via fisher, but Tide's interactive wizard is
  not launched from Go. Shellup writes a small set of guarded defaults to
  `~/.config/fish/conf.d/shellup-tide.fish`; run
  `fish -c 'tide configure'` any time to open the real configurator. Your own
  choices win over the defaults.
- The custom prompt is a deterministic template based on four answers
  (Nerd Font icons, git status, light/dark theme, command duration). It is
  not a full-featured prompt; features are intentionally minimal.

## Project structure

```
cmd/shellup/         Entry point: setup, restore, version, help
internal/app/        Orchestration and restore flow, summary rendering
internal/backup/     Detection, timestamped backups, manifest, restore
internal/env/        OS release parsing, package manager and AUR detection
internal/fish/       conf.d/shellup.fish writer, config.fish, /etc/shells, chsh
internal/pkgmgr/     pacman/apt/dnf/AUR wrapper, declarative package table
internal/prompt/     starship/tide/custom prompt templates
internal/aliases/    Optional alias questions and statement rendering
internal/ui/         bubbletea/lipgloss confirm and select dialogs
```

## How to run tests

Go 1.22 or newer is required.

```
go build ./...
go vet ./...
go test ./...
```

Unit tests cover environment parsing, each package manager (through a mock
command runner), the backup engine (against `t.TempDir()` homes, including the
restore pre-backup), the fish configuration writer, prompt templates and alias
rendering. Tests never touch the real user home or run real package manager
commands.
