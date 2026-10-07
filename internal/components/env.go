package components

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Env is the machine being backed up or restored to. Everything the
// components touch goes through it, so tests can point it at a temp folder.
type Env struct {
	Home       string
	ConfigHome string // ~/.config
	DataHome   string // ~/.local/share
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

	backedUp map[string]bool
}

// NewEnv describes the machine this is running on.
func NewEnv() *Env {
	home, _ := os.UserHomeDir()
	e := &Env{
		Home:       home,
		ConfigHome: envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")),
		DataHome:   envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
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
	e.PlasmaVersion = detectPlasmaVersion()
	fmt.Sscanf(e.PlasmaVersion, "%d", &e.PlasmaMajor)
	return e
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// detectPlasmaVersion asks plasmashell ("plasmashell 6.2.4"), returning ""
// when it can't tell.
func detectPlasmaVersion() string {
	out, err := exec.Command("plasmashell", "--version").Output()
	if err != nil {
		return ""
	}
	v, _ := strings.CutPrefix(strings.TrimSpace(string(out)), "plasmashell ")
	return v
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

func (e *Env) plasma6() bool { return e.PlasmaMajor == 0 || e.PlasmaMajor >= 6 }

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
