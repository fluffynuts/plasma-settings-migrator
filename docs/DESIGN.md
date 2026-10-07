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
| `internal/live` | The `Session` for the real machine: stops/starts the shortcut daemon, applies the color scheme live. |
| `internal/ui` | `Prompter` interface and the huh implementation. |
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
2. **Shortcut daemon.** `kglobalacceld` keeps shortcuts in memory and can write
   them back over our changes, so for hotkeys restores we stop it, write, and
   start it again (`systemctl --user stop/start plasma-kglobalaccel.service`,
   falling back to `kquitapp6/5` and a D-Bus ping). This also activates the new
   shortcuts at once, in "next login" mode too.
3. **"Apply now" is minimal**: only the color scheme, via
   `plasma-apply-colorscheme`. Icons, fonts, widget style and Gtk style show for
   new apps now and everywhere after re-login. Whether kde-gtk-config's kded
   module re-syncs Gtk settings from kdeglobals over what we write is
   **unverified**.
4. On restore to Plasma 5, service shortcuts are written as
   `keys,none,<app name>` triplets under `[x.desktop]`. Unverified on a real
   Plasma 5.

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
- Cursor theme, wallpapers, panel/widget layout
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
