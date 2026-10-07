package flow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
	"github.com/fluffynuts/plasma-settings-migrator/internal/ui"
)

// script answers prompts in order; nil answers take the defaults (what is
// ticked to start with).
type script struct {
	t       *testing.T
	answers []any
	asked   []string
	options [][]ui.Option
}

func (s *script) next(title string, opts []ui.Option) any {
	s.asked = append(s.asked, title)
	s.options = append(s.options, opts)
	if len(s.answers) == 0 {
		s.t.Fatalf("unexpected prompt %q", title)
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a
}

func (s *script) MultiSelect(title string, opts []ui.Option) ([]string, error) {
	a := s.next(title, opts)
	if a == nil {
		var keys []string
		for _, o := range opts {
			if o.Selected {
				keys = append(keys, o.Key)
			}
		}
		return keys, nil
	}
	return a.([]string), nil
}
func (s *script) Select(title string, opts []ui.Option) (string, error) {
	return s.next(title, opts).(string), nil
}
func (s *script) Confirm(title string) (bool, error)      { return s.next(title, nil).(bool), nil }
func (s *script) Input(title, def string) (string, error) { return s.next(title, nil).(string), nil }

func testEnv(t *testing.T) *components.Env {
	home := t.TempDir()
	return &components.Env{
		Home:        home,
		ConfigHome:  filepath.Join(home, ".config"),
		DataHome:    filepath.Join(home, ".local", "share"),
		PlasmaMajor: 6,
		BackupDir:   filepath.Join(home, "backups"),
		LookPath:    func(string) (string, error) { return "/bin/x", nil },
	}
}

func put(t *testing.T, path, content string) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type fakeSession struct {
	subs []string
	now  bool
}

func (f *fakeSession) Begin(subs []string, now bool) (func() []string, error) {
	f.subs, f.now = subs, now
	return func() []string { return []string{"a note"} }, nil
}

func TestBackupThenRestore(t *testing.T) {
	src, dst := testEnv(t), testEnv(t)
	put(t, filepath.Join(src.ConfigHome, "kdeglobals"), "[General]\nfont=Noto,10\nfixed=Hack,10\n\n[Icons]\nTheme=Papirus\n")
	put(t, filepath.Join(src.ConfigHome, "kglobalshortcutsrc"),
		"[kwin]\n_k_friendly_name=KWin\nShow Desktop=Meta+D,Meta+D,Peek\nOverview=Meta+W,Meta+W,Overview\n"+
			"Mine=Meta+M,Meta+X,Mine\nMine 2=Meta+N,Meta+Y,Mine 2\n")
	zipPath := filepath.Join(t.TempDir(), "b.zip")

	var out bytes.Buffer
	backup := &script{t: t, answers: []any{
		[]string{"theme", "hotkeys"},    // categories
		[]string{"fonts", "icon-theme"}, // theme subs
		[]string{"global-shortcuts"},    // hotkey subs
		[]string{"kwin/Mine"},           // items: narrowed to one of the two offered
	}}
	got, err := Backup(BackupOptions{Env: src, Prompt: backup, Out: &out, ZipPath: zipPath})
	if err != nil || got != zipPath {
		t.Fatalf("%v %q\n%s", err, got, out.String())
	}
	// only customised shortcuts are offered, all ticked
	items := backup.options[3]
	if len(items) != 2 || !items[0].Selected || !strings.Contains(items[0].Label, "KWin › ") {
		t.Errorf("item options: %+v", items)
	}
	// top-level and sub choices start unticked
	for _, o := range backup.options[0] {
		if o.Selected {
			t.Errorf("%q starts ticked", o.Label)
		}
	}

	out.Reset()
	sess := &fakeSession{}
	restore := &script{t: t, answers: []any{
		[]string{"theme", "hotkeys"}, // only categories present in the backup are offered
		[]string{"fonts"},            // icon-theme left out
		[]string{"global-shortcuts"},
		nil,   // items: the one in the backup, ticked
		"now", // apply mode
	}}
	err = Restore(RestoreOptions{Env: dst, Prompt: restore, Out: &out, ZipPath: zipPath, Session: sess})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !sess.now || strings.Join(sess.subs, ",") != "theme/fonts,hotkeys/global-shortcuts" {
		t.Errorf("session: %+v", sess)
	}
	if b, _ := os.ReadFile(filepath.Join(dst.ConfigHome, "kdeglobals")); !strings.Contains(string(b), "font=Noto,10") || strings.Contains(string(b), "Papirus") {
		t.Errorf("kdeglobals:\n%s", b)
	}
	b, _ := os.ReadFile(filepath.Join(dst.ConfigHome, "kglobalshortcutsrc"))
	if !strings.Contains(string(b), "Mine=Meta+M,Meta+X,Mine") || strings.Contains(string(b), "Mine 2") {
		t.Errorf("shortcuts:\n%s", b)
	}
	if !strings.Contains(out.String(), "a note") {
		t.Errorf("output lacks session note:\n%s", out.String())
	}
	if len(restore.options[0]) != 2 {
		t.Errorf("offered categories: %+v", restore.options[0])
	}
}

func TestBackupWithNothingSelected(t *testing.T) {
	var out bytes.Buffer
	p := &script{t: t, answers: []any{[]string{}}}
	got, err := Backup(BackupOptions{Env: testEnv(t), Prompt: p, Out: &out})
	if err != nil || got != "" || !strings.Contains(out.String(), "Nothing selected") {
		t.Errorf("%v %q %s", err, got, out.String())
	}
}

func TestRestoreRejectsANonBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(p, []byte("nope"), 0o644)
	err := Restore(RestoreOptions{Env: testEnv(t), Prompt: &script{t: t}, Out: &bytes.Buffer{}, ZipPath: p})
	if err == nil {
		t.Error("accepted")
	}
}
