# Shellup

Shellup is an interactive CLI tool that sets up a fish-based developer
environment: it installs a set of modern shell utilities, configures the
prompt, offers optional aliases and switches the default login shell.

Shellup is safe by default. It works in a clean room: the entire fish
configuration is moved into a timestamped backup before a fresh environment is
created, and `shellup restore` puts the original back. Deleting the old
configuration always requires an explicit confirmation in a bubbletea dialog.

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
- Configures the prompt from one of three interactive choices: `starship`,
  `tide` (installed through fisher), or `none` (leave the prompt untouched).
- Offers optional aliases as separate yes/no questions: a zoxide-aware `cd`
  function that falls back to the builtin cd, `cat` to `bat --paging=never`,
  `ls` to `eza --icons=always`.
- Always disables fish's default greeting with `set -g fish_greeting` and
  initializes zoxide with `zoxide init fish | source`. The zoxide hook records
  every directory change in its database; initialization only defines `z`,
  `zi` and the `PWD` hook, so the optional `cd` function stays in charge. A
  greeting set in your own `config.fish` still wins, because fish loads it
  after `conf.d/`.
- Builds a clean room instead of merging with an existing setup: the whole
  `~/.config/fish` directory is moved into a timestamped backup, deleted, and
  recreated empty. Shellup then writes only its own files, including
  `conf.d/shellup.fish` (regenerated in full on every run) and
  `conf.d/shellup-tide.fish` when tide is chosen. No file from the previous
  configuration, including `config.fish`, is copied over.
- Adds fish to `/etc/shells` (asked first, done via `sudo tee -a`) and offers
  to switch the login shell with `chsh -s /path/to/fish`.
- Prints a summary at the end listing every file written, every package
  installed and everything that was skipped, with reasons.

## Installation

Three installation methods are available. Pick the one that matches your
system.

1. Download a pre-built binary (recommended for fresh systems)

If Go is not yet installed on the machine, download a pre-built binary from
the GitHub Releases page. Binaries are published for Linux `amd64` and
`arm64` (see the note below on producing them; the process is
cross-platform only for Linux).

```bash
# Example for x86_64 Linux
curl -L -o shellup https://github.com/mrdyuke/shellup/releases/latest/download/shellup-linux-amd64
chmod +x shellup
sudo mv shellup /usr/local/bin/
```

2. Install via `go install` (quickest for Go users)

If Go is already installed, install the latest version directly:

```bash
go install github.com/mrdyuke/shellup/cmd/shellup@latest
```

The binary lands in `$GOPATH/bin` or `$HOME/go/bin`; make sure that
directory is in your `$PATH`.

3. Build from source (for development)

```bash
git clone https://github.com/mrdyuke/shellup.git
cd shellup
go build -o shellup ./cmd/shellup
sudo mv shellup /usr/local/bin/
```

Note on cross-platform binaries: release binaries are built for Linux
`amd64` and `arm64` only. The repository ships `build.sh` in the project
root, which runs the builds with `CGO_ENABLED=0` for static linking and
writes both binaries into `dist/`. macOS and Windows targets are out of
scope by design.

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

## Clean room: backup and restore

Shellup does not merge its configuration with an existing fish setup. It
creates a predictable clean room:

1. Shellup asks for confirmation and moves the complete `~/.config/fish`
   directory, recursively, into

   ```
   ~/.config/shellup-backups/<timestamp>/fish
   ```

2. `~/.config/starship.toml` is copied into the same backup directory when it
   exists. It stays in place, because only fish configuration is isolated.
3. The manifest records the current login shell and, when fisher is installed,
   the list of Fisher plugins. Both live outside the managed file set, so they
   cannot be derived from the files alone.
4. The original `~/.config/fish` directory is deleted and a fresh one is
   created. Shellup writes only its own files: `conf.d/shellup.fish`,
   `conf.d/shellup-tide.fish` when the tide prompt is chosen, and a minimal
   `config.fish` for personal settings. No file from the previous
   configuration, including `config.fish`, is carried into the clean room.

Each backup directory contains a `manifest.json` listing every saved item,
whether it was moved or copied, plus the recorded shell state. Backups are
plain files and can also be inspected or restored by hand.

Restore command:

```
shellup restore
```

It lists the available backups, shows the selected manifest, and after an
explicit confirmation:

- removes the `IlanCosman/tide` Fisher plugin from the clean room when fisher
  is installed, before the original files come back;
- deletes the current clean-room `~/.config/fish` and restores the backed-up
  `fish` directory (the backup itself stays intact);
- copies `starship.toml` back when the backup contains it;
- runs `chsh -s <previous shell>` when the login shell differs from the one in
  the manifest.

The summary reports every restored file and every command that ran. If `chsh`
fails, Shellup prints the exact manual command. Because only one backup is
chosen, configuration changes made inside the clean room are not preserved by a
restore; earlier backups remain on disk.

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
- The `ls` alias passes `--icons=always`, which recent eza versions require to
  keep icons on and to stop `--icons` from consuming the first path argument.
  Icons need a Nerd Font in the terminal; without one eza prints replacement
  glyphs. Because of `always`, icons also appear when the output is piped.
- The clean room replaces the whole fish directory. Personal files under
  `~/.config/fish` are reachable only through `shellup restore`; changes made
  inside the clean room are discarded when a backup is restored.
- Restoring the login shell runs `chsh`, so PAM may ask for your password. A
  shell that is not listed in `/etc/shells` still fails, exactly like the
  initial switch.

## Project structure

```
cmd/shellup/         Entry point: setup, restore, version, help
internal/app/        Orchestration and restore flow, summary rendering
internal/backup/     Clean-room move, timestamped backups, manifest, restore
internal/env/        OS release parsing, package manager and AUR detection
internal/fish/       conf.d/shellup.fish writer, config.fish, /etc/shells, chsh
internal/pkgmgr/     pacman/apt/dnf/AUR wrapper, declarative package table
internal/prompt/     starship/tide prompt templates
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
command runner), the backup engine (against `t.TempDir()` homes, including
moving the fish directory, clean-room isolation and restore of files plus
shell state), the fish configuration writer, prompt templates and alias
rendering. Tests never touch the real user home or run real package manager
commands.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
