package live

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
)

func newSession(t *testing.T, fail string) (*Session, *[]string) {
	home := t.TempDir()
	program := filepath.Join(home, "prog")
	os.WriteFile(program, []byte("PROGRAM"), 0o755)
	var ran []string
	return &Session{
		Env: &components.Env{
			Home: home, ConfigHome: filepath.Join(home, ".config"), DataHome: filepath.Join(home, ".local", "share"),
			PlasmaMajor: 6, LookPath: func(string) (string, error) { return "/bin/x", nil },
		},
		Program: program,
		Run: func(name string, args ...string) error {
			cmd := name + " " + strings.Join(args, " ")
			ran = append(ran, cmd)
			if fail != "" && strings.Contains(cmd, fail) {
				return errors.New("failed")
			}
			return nil
		},
	}, &ran
}

func TestHotkeysNowStopAndRestartTheDaemon(t *testing.T) {
	s, ran := newSession(t, "")
	end, _ := s.Begin([]string{"hotkeys/global-shortcuts"}, true)
	if s.Env.ShortcutsAtLogin {
		t.Error("shortcuts left for login, with the daemon stopped")
	}
	notes := end()
	got := strings.Join(*ran, "\n")
	for _, want := range []string{"is-active --quiet plasma-kglobalaccel.service", "stop plasma-kglobalaccel.service", "start plasma-kglobalaccel.service"} {
		if !strings.Contains(got, want) {
			t.Errorf("didn't run %q:\n%s", want, got)
		}
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "restarted") {
		t.Errorf("notes: %v", notes)
	}
}

func TestHotkeysOnWaylandWaitForLogin(t *testing.T) {
	// The daemon is inside KWin: nothing is stopped, the shortcuts go to
	// the login hook.
	s, ran := newSession(t, "")
	s.Wayland = true
	end, _ := s.Begin([]string{"hotkeys/global-shortcuts"}, true)
	if !s.Env.ShortcutsAtLogin {
		t.Fatal("shortcuts would be written under KWin")
	}
	if len(*ran) != 0 {
		t.Errorf("ran %v", *ran)
	}
	// what the component does with ShortcutsAtLogin, in short
	os.MkdirAll(components.PendingDir(s.Env), 0o755)
	os.WriteFile(filepath.Join(components.PendingDir(s.Env), "shortcuts.json"), []byte("{}"), 0o644)
	notes := strings.Join(end(), "\n")
	if !strings.Contains(notes, "next log in") || !strings.Contains(notes, "in memory") {
		t.Errorf("notes: %s", notes)
	}
	if _, err := os.Stat(filepath.Join(s.Env.ConfigHome, "plasma-workspace", "env", "plasma-settings-migrator.sh")); err != nil {
		t.Errorf("no login hook: %v", err)
	}
}

func TestHotkeysForNextLoginLeaveTheDaemonAlone(t *testing.T) {
	s, ran := newSession(t, "")
	end, _ := s.Begin([]string{"hotkeys/global-shortcuts"}, false)
	end()
	if !s.Env.ShortcutsAtLogin || len(*ran) != 0 {
		t.Errorf("at login %v, ran %v", s.Env.ShortcutsAtLogin, *ran)
	}
}

func TestDaemonNotRunningMeansLogin(t *testing.T) {
	s, ran := newSession(t, "is-active")
	end, _ := s.Begin([]string{"hotkeys/global-shortcuts"}, true)
	end()
	for _, c := range *ran {
		if strings.Contains(c, " start ") || strings.Contains(c, " stop ") || strings.HasPrefix(c, "kquitapp") {
			t.Errorf("ran %q", c)
		}
	}
	if !s.Env.ShortcutsAtLogin {
		t.Error("shortcuts would be written under a running daemon")
	}
}

func TestPlasma5WithoutSystemdQuitsItsDaemon(t *testing.T) {
	s, ran := newSession(t, "is-active")
	s.Env.PlasmaMajor = 5
	s.Begin([]string{"hotkeys/global-shortcuts"}, true)
	if !strings.Contains(strings.Join(*ran, "\n"), "kquitapp5 kglobalaccel5") || s.Env.ShortcutsAtLogin {
		t.Errorf("ran %v", *ran)
	}
}

func TestThemeOnlyTouchesNoDaemon(t *testing.T) {
	s, ran := newSession(t, "")
	end, _ := s.Begin([]string{"theme/color-scheme"}, true)
	if notes := end(); len(*ran) != 0 || len(notes) != 0 || s.Env.ShortcutsAtLogin {
		t.Errorf("ran %v notes %v", *ran, notes)
	}
}
