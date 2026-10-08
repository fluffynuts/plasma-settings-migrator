package components

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fluffynuts/plasma-settings-migrator/internal/kconfig"
)

// Env is the machine being backed up or restored to. Everything the
// components touch goes through it, so tests can point it at a temp folder.
type Env struct {
	Home       string
	ConfigHome string // ~/.config
	DataHome   string // ~/.local/share
	CacheHome  string // ~/.cache; "" leaves caches alone
	// DataDirs are the system-wide data folders (/usr/share, ...), searched
	// for installed applications.
	DataDirs []string
	// PlasmaMajor is 5 or 6 (0 if unknown, which is treated as 6).
	PlasmaMajor int
	// PlasmaVersion is the full version ("6.2.4"), when known.
	PlasmaVersion string
	LookPath      func(string) (string, error)
	// BackupDir is where files are saved before being overwritten. It is
	// created on first use.
	BackupDir string

	// ApplyNow is set when a restore's changes are wanted in the running
	// session: then Plasma's own tools (plasma-apply-colorscheme, ...) do
	// the work where there is one, and running programs are told.
	ApplyNow bool
	// ShortcutsAtLogin makes shortcuts be saved for the login hook (see
	// pending.go) instead of written to kglobalshortcutsrc now, when the
	// shortcut daemon holding them can't be stopped.
	ShortcutsAtLogin bool
	// Run runs a program, returning its error. Defaults to exec.
	Run func(name string, args ...string) error

	backedUp map[string]bool
}

func (e *Env) run(name string, args ...string) error {
	if e.Run != nil {
		return e.Run(name, args...)
	}
	return exec.Command(name, args...).Run()
}

func (e *Env) have(name string) bool {
	look := e.LookPath
	if look == nil {
		look = exec.LookPath
	}
	_, err := look(name)
	return err == nil
}

// findData looks for a file or folder (relative, slash form) in the user's
// data folder, then the system's, returning its path or "".
func (e *Env) findData(rel string) string {
	for _, d := range append([]string{e.DataHome}, e.DataDirs...) {
		p := filepath.Join(d, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// notify sends a D-Bus signal to running programs, best effort: the
// change is in their config files whether or not they hear about it.
func (e *Env) notify(path, member string, args ...string) {
	if !e.have("dbus-send") {
		return
	}
	e.run("dbus-send", append([]string{"--session", "--type=signal", path, member}, args...)...)
}

// NewEnv describes the machine this is running on.
func NewEnv() *Env {
	home, _ := os.UserHomeDir()
	e := &Env{
		Home:       home,
		ConfigHome: envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")),
		DataHome:   envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
		CacheHome:  envOr("XDG_CACHE_HOME", filepath.Join(home, ".cache")),
		LookPath:   exec.LookPath,
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	e.DataDirs = append(strings.Split(dirs, ":"),
		"/var/lib/flatpak/exports/share",
		filepath.Join(e.DataHome, "flatpak", "exports", "share"),
		"/var/lib/snapd/desktop")
	e.BackupDir = filepath.Join(e.DataHome, "plasma-settings-migrator", "backups", time.Now().Format("20060102-150405"))
	e.PlasmaVersion = detectPlasmaVersion(e.DataDirs)
	fmt.Sscanf(e.PlasmaVersion, "%d", &e.PlasmaMajor)
	return e
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// detectPlasmaVersion finds the installed Plasma's version ("6.2.4"),
// returning "" when it can't tell.
//
// The login session files plasma-workspace installs (xsessions/ and
// wayland-sessions/: plasma.desktop, plasmax11.desktop, plasmawayland.desktop,
// depending on the version) carry it as X-KDE-PluginInfo-Version, and need
// no display to read, unlike plasmashell --version, which is only the
// fallback: it fails over ssh, for one.
func detectPlasmaVersion(dataDirs []string) string {
	if v := sessionFileVersion(dataDirs); v != "" {
		return v
	}
	out, err := exec.Command("plasmashell", "--version").Output()
	if err != nil {
		return ""
	}
	v, _ := strings.CutPrefix(strings.TrimSpace(string(out)), "plasmashell ")
	return v
}

func sessionFileVersion(dataDirs []string) string {
	for _, d := range dataDirs {
		for _, sessions := range []string{"wayland-sessions", "xsessions"} {
			matches, _ := filepath.Glob(filepath.Join(d, sessions, "*.desktop"))
			for _, m := range matches {
				f, err := kconfig.ReadFile(m)
				if err != nil {
					continue
				}
				// Only Plasma's own: other desktops' files may carry the key too.
				cmd, _ := f.Get("Desktop Entry", "Exec")
				if !strings.Contains(cmd, "startplasma") {
					continue
				}
				if v, _ := f.Get("Desktop Entry", "X-KDE-PluginInfo-Version"); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

// RootPath is the folder a FileRef's Root names.
func (e *Env) RootPath(root string) string {
	switch root {
	case "config":
		return e.ConfigHome
	case "data":
		return e.DataHome
	default:
		return e.Home
	}
}

// FilePath is where a FileRef lives on this machine.
func (e *Env) FilePath(ref FileRef) string {
	return filepath.Join(e.RootPath(ref.Root), filepath.FromSlash(ref.Path))
}

// backup saves a copy of path (if it exists) before it is overwritten, once
// per run, under BackupDir with the path it had below the home folder.
func (e *Env) backup(path string) error {
	if e.backedUp[path] {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	rel, err := filepath.Rel(e.Home, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = strings.TrimLeft(path, "/")
	}
	if err := copyFile(path, filepath.Join(e.BackupDir, rel)); err != nil {
		return fmt.Errorf("backing up %s: %w", path, err)
	}
	if e.backedUp == nil {
		e.backedUp = map[string]bool{}
	}
	e.backedUp[path] = true
	return nil
}
