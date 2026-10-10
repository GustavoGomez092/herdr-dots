# Install and upgrade

How to install Herdr Dots, check the install, upgrade it and remove it.

## From the Herdr marketplace

    herdr plugin install GustavoGomez092/herdr-dots

Herdr clones the repository and runs `scripts/build.sh` (`build.ps1` on
Windows). The script downloads the prebuilt binary for your OS and CPU from
the GitHub release, checks it against `release/SHA256SUMS` in the clone, and
installs it in `bin/`. With no release for this version, no network, or a
checksum that doesn't match, it builds from source with Go 1.25 or newer;
without Go it stops and says what to do. Nothing half-installed is left
behind. Set `HERDR_PLUGIN_BUILD_OFFLINE=1` to skip the download.

Check an install: `./bin/herdr-dots --version` says `release` or `source`
and the commit; `./bin/herdr-dots verify-release` re-checks the kept archive
against `release/SHA256SUMS`, checks that the installed binary is the one in
the archive and, if `cosign` is installed and the release had a signature
bundle, the release signature. cosign is optional: without it the command
says the signature was not checked and verifies checksums only. For a source
build it says there is no release archive to verify.

Targets: macOS and Linux on amd64 and arm64, Windows on amd64; Windows arm64
binaries are built but not tested. The PowerShell install (`build.ps1`) has
not been tested here.

## From source (development)

    git clone <repository> && cd herdr-dots
    make build
    herdr --session <a test session> plugin link .

`plugin link` runs no build step, so run `make build` after every change.
Links are global: use a separate Herdr session for testing.

On Windows, if your machine's policy blocks PowerShell scripts, build by hand
with `go build -o bin\herdr-dots.exe .\cmd\herdr-dots` and link again.

The engine starts automatically with Herdr (startup hook), or the first time
you use a Dots action.

## Upgrading

Herdr has no `plugin update`: reinstall the plugin. Your dots, settings,
credentials, memory and history live in the plugin's config and state
directories and are kept. At start the engine checks every file's schema
version; older files are upgraded (the original is kept as
`<file>.bak-v<old>`), and a file written by a newer herdr-dots is left
read-only with a notice. `herdr-dots migrate --dry-run` shows what it would
do.

`herdr-dots migrate` upgrades the config and shared state, then every
session folder whose engine isn't running (a running engine migrates its own
session at its next start). A file is rewritten atomically, after its
original is saved as `<file>.bak-v<old>` (mode 0600). A failed migration
leaves the file untouched and read-only. Commands that write files (`keys`,
`dots`, `memory`, `setup-mcp`, `setup`, `manage`, `uninstall-extras`) treat a file from
a newer version as read-only too.

## Uninstall

    ./bin/herdr-dots uninstall-extras            # MCP entries and keybindings, with backups
    ./bin/herdr-dots uninstall-extras --purge    # also every dot, setting, credential and all memory
    herdr plugin uninstall herdr-dots

`uninstall-extras` removes only the lines the plugin added: its MCP server
entries for Claude Code and Codex, and its keybinding block in Herdr's
`config.toml`. For each, it shows exactly which lines go, asks first (`--yes`
skips that question), and saves a backup (`<file>.bak-herdr-dots-<time>`)
before editing. Everything else in those files stays. Keybindings in a syntax
it can't edit are reported so you can remove them by hand.

`--purge` also deletes the plugin's config and state directories: every dot,
setting, credential and all memory, with no undo. It always asks you to type
`purge herdr-dots` (`--yes` doesn't skip that). It refuses, and changes
nothing, when a plugin directory is a symlink, when a Dots engine is still
running (quit Herdr first), or when a directory doesn't look like the
plugin's own (not named `herdr-dots`, not tied to the other by the
`.state-dir` pointer, your home directory or a first-level folder of it, or one
that holds Herdr's own config).
