// Package live deals with the running desktop during a restore.
//
// The two things it does are best effort, and report problems as notes
// rather than failing the restore:
//
//   - kglobalacceld keeps the shortcuts in memory and writes them back to
//     kglobalshortcutsrc, so writing that file under it risks the change
//     being overwritten. It is stopped while hotkeys are restored, and
//     started again after, which also makes it read the new shortcuts.
//   - when the user wants the changes now, they are applied through the
//     tools Plasma has for that, where there is one. Anything else shows
//     on next login, and the notes say so.
package live

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
	"github.com/fluffynuts/plasma-settings-migrator/internal/kconfig"
)

// Session implements flow.Session for the machine it runs on.
type Session struct {
	Env *components.Env
	// Run runs a command, returning its error. Defaults to exec.
	Run func(name string, args ...string) error
}

func (s *Session) run(name string, args ...string) error {
	if s.Run != nil {
		return s.Run(name, args...)
	}
	return exec.Command(name, args...).Run()
}

func (s *Session) have(name string) bool {
	look := s.Env.LookPath
	if look == nil {
		look = exec.LookPath
	}
	_, err := look(name)
	return err == nil
}

const accelUnit = "plasma-kglobalaccel.service"

// Begin implements flow.Session.
func (s *Session) Begin(subs []string, applyNow bool) (func() []string, error) {
	has := func(prefix string) bool {
		for _, r := range subs {
			if strings.HasPrefix(r, prefix) {
				return true
			}
		}
		return false
	}
	stopped := false
	if has("hotkeys/") {
		stopped = s.stopShortcutDaemon()
	}
	return func() []string {
		var notes []string
		if stopped {
			if s.startShortcutDaemon() {
				notes = append(notes, "Shortcuts: restarted the shortcut daemon, so they are active now.")
			} else {
				notes = append(notes, "Shortcuts: couldn't restart the shortcut daemon; they will be active after you log out and in again.")
			}
		}
		if applyNow {
			notes = append(notes, s.applyThemeNow(subs)...)
		} else if has("theme/") {
			notes = append(notes, "Theme: changes show up for new applications now, and everywhere after you log out and in again.")
		}
		return notes
	}, nil
}

// stopShortcutDaemon reports whether it stopped one that was running.
func (s *Session) stopShortcutDaemon() bool {
	if s.have("systemctl") && s.run("systemctl", "--user", "is-active", "--quiet", accelUnit) == nil {
		return s.run("systemctl", "--user", "stop", accelUnit) == nil
	}
	// Plasma 5, or a session not run by systemd.
	for _, quit := range [][]string{{"kquitapp6", "kglobalaccel"}, {"kquitapp5", "kglobalaccel5"}} {
		if s.have(quit[0]) && s.run(quit[0], quit[1]) == nil {
			return true
		}
	}
	return false
}

func (s *Session) startShortcutDaemon() bool {
	if s.have("systemctl") && s.run("systemctl", "--user", "start", accelUnit) == nil {
		return true
	}
	// D-Bus starts it on demand.
	return s.have("dbus-send") && s.run("dbus-send", "--session", "--type=method_call",
		"--dest=org.kde.kglobalaccel", "/kglobalaccel", "org.freedesktop.DBus.Peer.Ping") == nil
}

func (s *Session) applyThemeNow(subs []string) []string {
	var notes []string
	var themed []string
	for _, r := range subs {
		if rest, ok := strings.CutPrefix(r, "theme/"); ok {
			themed = append(themed, rest)
		}
	}
	if len(themed) == 0 {
		return nil
	}
	pending := map[string]bool{}
	for _, t := range themed {
		pending[t] = true
	}
	if pending["color-scheme"] {
		delete(pending, "color-scheme")
		name := s.currentColorScheme()
		switch {
		case name == "":
		case !s.have("plasma-apply-colorscheme"):
			notes = append(notes, "Color scheme: plasma-apply-colorscheme isn't installed, so it shows after you log out and in again.")
		case s.run("plasma-apply-colorscheme", name) != nil:
			notes = append(notes, fmt.Sprintf("Color scheme: applying %q now failed; it shows after you log out and in again.", name))
		default:
			notes = append(notes, fmt.Sprintf("Color scheme: %q applied now.", name))
		}
	}
	if len(pending) > 0 {
		notes = append(notes, "Icons, fonts, widget style and Gtk style can't be applied to a running session from here: they show for new applications now, and everywhere after you log out and in again.")
	}
	return notes
}

func (s *Session) currentColorScheme() string {
	f, err := kconfig.ReadFile(filepath.Join(s.Env.ConfigHome, "kdeglobals"))
	if err != nil {
		return ""
	}
	v, _ := f.Get("General", "ColorScheme")
	return v
}
