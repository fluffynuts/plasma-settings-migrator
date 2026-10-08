plasma-settings-migrator
---

Back up KDE Plasma settings into a zip file and restore them on another machine. Linux only.

## Use

```sh
plasma-settings-migrator backup [settings.zip]    # on the machine you're copying from
plasma-settings-migrator restore settings.zip     # on the machine you're copying to
```

Both are interactive: first pick categories, then what to include from each (`x` ticks, `enter`
continues, `/` filters long lists). Currently:

| Category | Sub-components |
|---|---|
| Theme | Global theme, Plasma style, Color scheme, Icon theme, Fonts, Widget style, Gtk style |
| Hotkeys | Global shortcuts, Custom hotkeys |

For hotkeys you then get a list of the shortcuts you have **changed from their defaults**, all ticked.
Shortcuts for applications are restored only if the application is installed on the target, and
custom command shortcuts only if the program they run is. Files that a restore replaces are first
copied to `~/.local/share/plasma-settings-migrator/backups/<timestamp>/`.

A restore asks whether to apply the changes now or at your next login. "Now" uses Plasma's own tools
(`plasma-apply-lookandfeel`, `plasma-apply-colorscheme`, ...) and tells running applications; some
only show the change once restarted. On Plasma 6 with Wayland, shortcuts always wait for your next
login, because KWin holds them in memory and would write over them. A one-off script in
`~/.config/plasma-workspace/env/` puts them in place before KWin starts, then removes itself (its
log is `~/.local/share/plasma-settings-migrator/login.log`).

Plasma 5 and 6 backups can be restored on either. Settings are merged key by key into the target's
files, so anything not in the backup is left alone.

### One file to copy: bundles

At the end of `backup` you're asked whether to save a **bundle** as well: a copy of this program
with the backup inside it. Copy that one file to the other machine and run it; with no command it
restores its own backup.

```sh
plasma-settings-migrator backup --bundle migrate-settings   # don't ask, save the bundle here
plasma-settings-migrator bundle settings.zip [migrate-settings]   # bundle a zip you already have
./migrate-settings                                          # on the other machine
```

A bundle only runs on the same kind of machine as the program that made it (x86-64 or ARM). It is
still a zip too: `restore migrate-settings` and `unzip -l migrate-settings` both read it.
`./migrate-settings --install` installs only the program, as `plasma-settings-migrator`, without the
backup.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/fluffynuts/plasma-settings-migrator/master/install.sh | sh
```

This downloads the latest release for your machine, checks it against the release's
checksums, and runs `plasma-settings-migrator --install` from it. That puts the `plasma-settings-migrator` binary in `~/.local/bin`; if that folder isn't on your `PATH`, it tells you what to add to your shell's profile. `install.sh` needs `curl` or `wget`, and `unzip` (or `python3`).

Run the same command again to upgrade, or, once plasma-settings-migrator is installed:

```sh
plasma-settings-migrator --upgrade
```

You can also download the zip for your platform from the
[latest release](https://github.com/fluffynuts/plasma-settings-migrator/releases/latest) (`plasma-settings-migrator-linux-amd64.zip`,
`plasma-settings-migrator-linux-arm64.zip`, for x86-64 and ARM, plus a
`SHA256SUMS`), unzip it anywhere, and run `plasma-settings-migrator --install` from the folder it makes.
