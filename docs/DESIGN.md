# Design notes and status

Written 2026-10-07 so work can resume later. Nothing here is committed yet: the
repo has no commits, everything is untracked.

## What the tool is

Linux-only (KDE Plasma 5 and 6) CLI. `backup` collects chosen settings into a
zip; `restore` applies chosen parts of a zip on another machine. Both are
interactive (charmbracelet/huh): pick categories, then sub-components, then (for
some) individual items.

Windows and macOS support was dropped on purpose; the goscaffold leftovers for
them were removed.

## Layout

| Package | Job |
|---|---|
| `internal/kconfig` | KConfig INI parser/editor. Unmodified lines round-trip byte for byte; values are kept raw (escapes like `\t` are not decoded). Atomic `WriteFile`. |
| `internal/components` | The registry: categories -> sub-components. Each has `Collect` (machine -> `Fragment`) and `Apply` (`Fragment` -> machine). `theme.go`, `hotkeys.go`. |
| `internal/archive` | Zip read/write: `manifest.json`, `<cat>/<sub>/fragment.json`, `files/<root>/<path>`. Fragments are validated on read (no path escapes). |
| `internal/flow` | The backup and restore processes, written against a `ui.Prompter` and a `Session` so they test without a terminal. |
| `internal/live` | The `Session` for the real machine: stops/starts a separate shortcut daemon, or installs the login hook. Theme subs apply themselves (with Plasma's tools in "now" mode). |
| `internal/ui` | `Prompter` interface and the huh implementation. |
| `internal/bundle` | Single-file bundles: `<program><zip><trailer>`, the trailer being `PSMBUNDL` and the program's length (uint64, little-endian). `archive.Open` reads the zip in a bundle; `--install` copies only the program part. |
| `internal/appcli` | goscaffold's --help/--version/--install/--upgrade. |

A `Fragment` is plain data: `Items` (what the user can tick), `Keys` (KConfig
settings: file, group, key, value) and `Files` (files/folders under the
`config`, `data` or `home` roots). New sub-components usually only need a
`Collect`, and can use `applyGeneric` for `Apply`.

## Decisions made with the user

- Use `charmbracelet/huh`. Top-level and sub-component lists start **unticked**;
  the per-item list (hotkeys) starts **all ticked**.
- Settings are merged **key by key** (group/key level), never whole-file
  overwrites, except Gtk style which copies small files whole.
- Restore asks **apply now or on next login**. Files that get replaced are first
  copied to `~/.local/share/plasma-settings-migrator/backups/<timestamp>/`.
- Application shortcuts: restore only if the app (`.desktop` file) is installed
  on the target. Custom command shortcuts: create the `.desktop` file, or update
  the existing one of the same name, only if the program in `Exec` is found.
- Support Plasma 5 and 6 both (manifest records the version).
- Bundles: `backup` ends by offering one (or `--bundle <file>` saves one without
  asking; `bundle <zip> [<file>]` makes one from an existing zip). The zip is
  always saved as well. A bundle run with no command restores its own backup.

## Facts verified from KDE source (not memory)

- Color scheme is spread over kdeglobals: `Colors:*`, `ColorEffects:*`, `WM`
  colour keys, `General` (`ColorScheme`, `ColorSchemeHash`, `AccentColor`,
  `LastUsedCustomAccentColor`, `accentColorFromWallpaper`), `KDE`
  (`contrast`, `frameContrast`). A user-installed scheme is
  `~/.local/share/color-schemes/<name>.colors`.
- Fonts: `General` `font`, `fixed`, `smallestReadableFont`, `toolBarFont`,
  `menuFont`; `WM` `activeFont`. Icons: `Icons` `Theme`. Widget style: `KDE`
  `widgetStyle`, `unionStyle`, `ShowIconsOnPushButtons`, `ShowIconsInMenuItems`;
  `Toolbar style` `ToolButtonStyle`, `ToolButtonStyleOtherToolbars`.
- `kglobalshortcutsrc`: components (e.g. `[kwin]`) store actions as
  `active,default,friendly name`; the key separator is written as the two
  characters `\t`. Plasma 6 stores app launchers as `[services][x.desktop]` with
  `_launch=keys` only (overrides). Plasma 5 used top-level `[x.desktop]` triplets;
  Plasma 6 migrates them.
- Custom command shortcuts: a `.desktop` file with
  `X-KDE-GlobalAccel-CommandShortcut=true`, found in
  `~/.local/share/kglobalaccel/<uuid>.desktop` (when created by Plasma 6's
  migration; also check `applications/`), bound via `_launch` as above.
- `khotkeysrc` is legacy and is ignored.
- The huh toggle key is `x` (not space); `ctrl+a` selects all.

## Judgement calls to revisit

1. **Only customised shortcuts are listed** (active keys differ from default),
   because KDE writes every action and a full list would be hundreds of kwin
   rows. The user asked for "the relevant list, all selected"; confirm this is
   what they want, or add an "include defaults" option.
2. **Shortcuts on Plasma 6 Wayland wait for the next login** (see below), even
   when "now" is chosen. Applying them live would need kglobalaccel's D-Bus
   `setForeignShortcutKeys`, which takes `QKeySequence`s: every key name
   would have to be turned into Qt key codes. Not done.
3. **GTK sync unverified**: whether kde-gtk-config's kded module re-syncs Gtk
   settings from kdeglobals over what we write.

## Restoring into a running Plasma (verified from source 2026-10-08)

Found after a first real test (5.27 -> 6, and 6 -> 6) restored almost no
theme and only some shortcuts:

- **startplasma (5.27 and 6) re-applies the color scheme at login** when
  `[General] ColorSchemeHash` isn't the SHA-1 of the file of the scheme
  `[General] ColorScheme` names (default `BreezeLight`). Copying `Colors:*`
  alone gets undone. So the scheme's name is carried, `ColorSchemeHash` is
  not: on restore it is written empty when the scheme exists on the target
  (Plasma re-applies it from the target's file), or, when there's no scheme
  to come from, as the hash of the scheme Plasma would otherwise apply, so
  it leaves the colours be.
- **Global theme values live in `~/.config/kdedefaults/`** (written by
  startplasma for `[KDE] LookAndFeelPackage`); KConfig drops a user value
  equal to its default, so e.g. `ColorScheme=BreezeDark` is only there.
  Collecting falls back to kdedefaults. Writing `LookAndFeelPackage` makes
  the next login apply that theme's defaults.
- **`plasma-apply-colorscheme` / `plasma-apply-desktoptheme` do nothing when
  the setting already names the scheme** ("already set", exit 0), so in
  "now" mode they run *before* anything is written. `plasma-apply-lookandfeel
  --apply` (`lookandfeeltool` on older Plasma) has no such check and leaves
  the layout alone unless `--resetLayout`.
- **On Plasma 6 Wayland kglobalacceld runs inside KWin**
  (`kwin/src/globalshortcuts.cpp` makes a `KGlobalAccelD`), so
  `plasma-kglobalaccel.service` isn't it and can't be stopped; `kquitapp6
  kglobalaccel` would be asking KWin to quit, so it's never used. It never
  re-reads kglobalshortcutsrc, and every save (`Component::writeSettings`)
  deletes each group and writes it from memory. So the shortcuts go to
  `~/.local/share/plasma-settings-migrator/pending/` with a copy of the
  program, and `~/.config/plasma-workspace/env/plasma-settings-migrator.sh`
  runs `apply-pending` at the next login: startplasma sources those scripts
  (via `sh`, reading `env -0` from stdout, so the script must print nothing)
  before `setupPlasmaEnvironment` and before KWin starts. Where the daemon is
  separate (Plasma 5, Plasma 6 X11) and "now" is chosen, it is stopped,
  written under and started again, as before.
- **Plasma 5.27 already uses `[services][app.desktop]`** for application
  shortcuts, so that is what is written for every target.
- Unbound is `none` or empty: `none,,X` is not a customised shortcut.
- Running apps are told about icons, fonts and style with the D-Bus signals
  Plasma's KCMs send: `/KGlobalSettings org.kde.KGlobalSettings.notifyChange
  (type, 0)` (Palette 0, Font 1, Style 2, Icon 4, ToolbarStyle 6) and
  `/KIconLoader org.kde.KIconLoader.iconChanged(group)`, after removing
  `~/.cache/icon-cache.kcache`.

## Not tested against a real Plasma session

The sandbox this was built in has no KDE config. Unit tests use fixtures built
from the source analysis above, and the TUI was smoke-tested through a pty
against a fake home. The first thing to do on a real machine is run `backup`,
look at the lists, unzip the result and read the `fragment.json` files, then
`restore` on a second machine or a scratch `HOME`/`XDG_CONFIG_HOME`.

## Known gaps / ideas for next steps

- Font anti-aliasing/hinting/subpixel (fontconfig `~/.config/fontconfig/fonts.conf`
  and `kcmfonts`/`forceFontDPI`) are not included in Fonts.
- No handling of a restored key already being bound to something else on the
  target (conflict detection).
- Gtk style: only `gtk-3.0/settings.ini`, `gtk-4.0/settings.ini`, `~/.gtkrc-2.0`.
  GTK themes/icons installed by the user are not carried.
- Cursor theme and window decoration on their own (the global theme covers them), wallpapers, panel/widget layout
  (`plasma-org.kde.plasma.desktop-appletsrc`, the hard one: numeric IDs), kwin
  rules/effects, input devices, KRunner, Konsole, Dolphin etc. are not covered
  yet. Each would be a new sub-component in `internal/components`.
- Per-version key maps for Plasma 5 vs 6 (warn when a key doesn't exist there).
- Non-interactive mode (flags to select components) for scripting.
- Commit the work; set up the first commit on `main` (current branch is
  `master`, the repo's main branch is `main`).

## Handy commands

```sh
./make.sh check          # go vet + go test ./...
./make.sh build          # builds ./plasma-settings-migrator
# try it without touching real settings:
HOME=$(mktemp -d) XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share \
  ./plasma-settings-migrator restore some-backup.zip
```
