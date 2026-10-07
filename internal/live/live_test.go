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
	cfg := t.TempDir()
	os.WriteFile(filepath.Join(cfg, "kdeglobals"), []byte("[General]\nColorScheme=BreezeDark\n"), 0o644)
	var ran []string
	return &Session{
		Env: &components.Env{ConfigHome: cfg, LookPath: func(string) (string, error) { return "/bin/x", nil }},
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

func TestHotkeysStopAndRestartTheDaemon(t *testing.T) {
	s, ran := newSession(t, "")
	end, _ := s.Begin([]string{"hotkeys/global-shortcuts"}, false)
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

func TestDaemonNotRunningIsLeftAlone(t *testing.T) {
	s, ran := newSession(t, "is-active")
	fail := s.Run
	s.Run = func(name string, args ...string) error { // kquitapp fails when nothing is running, too
		if err := fail(name, args...); err != nil || strings.HasPrefix(name, "kquitapp") {
			return errors.New("not running")
		}
		return nil
	}
	end, _ := s.Begin([]string{"hotkeys/global-shortcuts"}, false)
	notes := end()
	for _, c := range *ran {
		if strings.Contains(c, " start ") || strings.Contains(c, " stop ") {
			t.Errorf("ran %q", c)
		}
	}
	if len(notes) != 0 {
		t.Errorf("notes: %v", notes)
	}
}

func TestThemeNowAppliesTheColorScheme(t *testing.T) {
	s, ran := newSession(t, "")
	end, _ := s.Begin([]string{"theme/color-scheme", "theme/fonts"}, true)
	notes := strings.Join(end(), "\n")
	if len(*ran) != 1 || (*ran)[0] != "plasma-apply-colorscheme BreezeDark" {
		t.Errorf("ran %v", *ran)
	}
	if !strings.Contains(notes, "applied now") || !strings.Contains(notes, "fonts") {
		t.Errorf("notes: %s", notes)
	}
}

func TestThemeOnLoginDoesNothingNow(t *testing.T) {
	s, ran := newSession(t, "")
	end, _ := s.Begin([]string{"theme/color-scheme"}, false)
	if notes := end(); len(*ran) != 0 || len(notes) != 1 || !strings.Contains(notes[0], "log out") {
		t.Errorf("ran %v notes %v", *ran, notes)
	}
}
