// Package live deals with the running desktop during a restore: the
// shortcut daemon.
//
// kglobalacceld keeps the shortcuts in memory and writes them back over
// kglobalshortcutsrc, so writing that file under it loses the change.
//
//   - Where it runs as a daemon of its own (Plasma 5, and Plasma 6 on X11)
//     and the changes are wanted now, it is stopped while the shortcuts are
//     written and started again after, which also makes it read them.
//   - Otherwise (Plasma 6 on Wayland runs it inside KWin, which can't be
//     stopped; or the user asked for next login) the shortcuts are written
//     at the next login, before KWin starts: see components' login hook.
//
// Theme changes are applied by the components themselves.
package live

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
)

// Session implements flow.Session for the machine it runs on.
type Session struct {
	Env *components.Env
	// Program is this program's file, copied for the login hook.
	Program string
	// Wayland is whether the session is a Wayland one ($XDG_SESSION_TYPE).
	Wayland bool
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
	hotkeys := false
	for _, r := range subs {
		hotkeys = hotkeys || strings.HasPrefix(r, "hotkeys/")
	}
	stopped := false
	if hotkeys {
		if applyNow {
			stopped = s.stopShortcutDaemon()
		}
		s.Env.ShortcutsAtLogin = !stopped
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
		if components.HasPendingShortcuts(s.Env) {
			if err := components.InstallLoginHook(s.Env, s.Program); err != nil {
				notes = append(notes, fmt.Sprintf("Shortcuts: couldn't set them up for your next login: %s", err))
			} else {
				why := ""
				if applyNow {
					why = " (Plasma keeps them in memory while you're logged in, and would write over them now)"
				}
				notes = append(notes, "Shortcuts: they will be put in place when you next log in"+why+
					". Log out and in again to get them; shortcuts you change before then are replaced by the restored ones.")
			}
		}
		return notes
	}, nil
}

// stopShortcutDaemon stops kglobalacceld where it is a daemon of its own,
// reporting whether it stopped one that was running. On Plasma 6 Wayland
// the D-Bus name org.kde.kglobalaccel belongs to KWin, so nothing that
// would quit whatever owns it is tried.
func (s *Session) stopShortcutDaemon() bool {
	if s.Wayland && s.Env.PlasmaMajor != 5 {
		return false // inside KWin
	}
	if s.have("systemctl") && s.run("systemctl", "--user", "is-active", "--quiet", accelUnit) == nil {
		return s.run("systemctl", "--user", "stop", accelUnit) == nil
	}
	if s.Env.PlasmaMajor == 5 && s.have("kquitapp5") {
		return s.run("kquitapp5", "kglobalaccel5") == nil
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
