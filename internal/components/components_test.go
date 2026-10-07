package components

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fluffynuts/plasma-settings-migrator/internal/kconfig"
)

func newTestEnv(t *testing.T) *Env {
	t.Helper()
	home := t.TempDir()
	return &Env{
		Home:        home,
		ConfigHome:  filepath.Join(home, ".config"),
		DataHome:    filepath.Join(home, ".local", "share"),
		DataDirs:    []string{filepath.Join(home, "usr-share")},
		PlasmaMajor: 6,
		BackupDir:   filepath.Join(home, "backups"),
		LookPath: func(name string) (string, error) {
			if name == "installed-tool" {
				return "/usr/bin/" + name, nil
			}
			return "", os.ErrNotExist
		},
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// archiveOf is what the zip layer will do: carry a fragment's files.
func archiveOf(t *testing.T, env *Env, f *Fragment) fs.FS {
	t.Helper()
	m := fstest.MapFS{}
	for _, ref := range f.Files {
		root := env.FilePath(ref)
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(env.RootPath(ref.Root), p)
			m[ArchiveFilesDir+"/"+ref.Root+"/"+filepath.ToSlash(rel)] = &fstest.MapFile{Data: []byte(read(t, p))}
			return nil
		})
	}
	return m
}

const sourceKdeglobals = `[ColorEffects:Disabled]
Color=56,56,56

[Colors:Window]
BackgroundNormal=1,2,3

[Colors:Window][Inactive]
BackgroundNormal=4,5,6

[General]
ColorScheme=MyScheme
TerminalApplication=konsole
font=Noto Sans,10
fixed=Hack,10

[Icons]
Theme=Papirus

[KDE]
widgetStyle=Breeze
SingleClick=false

[WM]
activeBackground=9,9,9
`

func TestThemeRoundTrip(t *testing.T) {
	src, dst := newTestEnv(t), newTestEnv(t)
	write(t, filepath.Join(src.ConfigHome, "kdeglobals"), sourceKdeglobals)
	write(t, filepath.Join(src.DataHome, "color-schemes", "MyScheme.colors"), "[Colors:View]\n")
	write(t, filepath.Join(src.DataHome, "icons", "Papirus", "index.theme"), "[Icon Theme]\n")
	write(t, filepath.Join(src.ConfigHome, "gtk-3.0", "settings.ini"), "[Settings]\ngtk-theme-name=Breeze\n")
	write(t, filepath.Join(dst.ConfigHome, "kdeglobals"), "[General]\nTerminalApplication=xterm\nfont=Old,9\n\n[KDE]\nSingleClick=true\n")

	for _, id := range []string{"color-scheme", "icon-theme", "fonts", "widget-style", "gtk-style"} {
		sub := Find("theme", id)
		frag, err := sub.Collect(src)
		if err != nil {
			t.Fatal(id, err)
		}
		if _, err := sub.Apply(dst, frag, archiveOf(t, src, frag), nil); err != nil {
			t.Fatal(id, err)
		}
	}

	got, _ := kconfig.ReadFile(filepath.Join(dst.ConfigHome, "kdeglobals"))
	for _, c := range [][3]string{
		{"Colors:Window", "BackgroundNormal", "1,2,3"},
		{"Colors:Window][Inactive", "BackgroundNormal", "4,5,6"},
		{"ColorEffects:Disabled", "Color", "56,56,56"},
		{"General", "ColorScheme", "MyScheme"},
		{"General", "font", "Noto Sans,10"},
		{"General", "fixed", "Hack,10"},
		{"Icons", "Theme", "Papirus"},
		{"KDE", "widgetStyle", "Breeze"},
		{"WM", "activeBackground", "9,9,9"},
		// untouched: not part of any theme component
		{"General", "TerminalApplication", "xterm"},
		{"KDE", "SingleClick", "true"},
	} {
		if v, _ := got.Get(c[0], c[1]); v != c[2] {
			t.Errorf("[%s] %s = %q, want %q", c[0], c[1], v, c[2])
		}
	}
	for _, p := range []string{
		filepath.Join(dst.DataHome, "color-schemes", "MyScheme.colors"),
		filepath.Join(dst.DataHome, "icons", "Papirus", "index.theme"),
		filepath.Join(dst.ConfigHome, "gtk-3.0", "settings.ini"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("not restored: %v", err)
		}
	}
	// the file that was overwritten was saved first
	if b := read(t, filepath.Join(dst.BackupDir, ".config", "kdeglobals")); !strings.Contains(b, "font=Old,9") {
		t.Errorf("backup of kdeglobals is wrong:\n%s", b)
	}
}

func TestThemeCollectsNothingFromAnEmptyMachine(t *testing.T) {
	env := newTestEnv(t)
	for _, c := range Registry() {
		for _, sub := range c.Subs {
			frag, err := sub.Collect(env)
			if err != nil || len(frag.Keys)+len(frag.Files)+len(frag.Items) != 0 {
				t.Errorf("%s/%s: %v %+v", c.ID, sub.ID, err, frag)
			}
		}
	}
}

const sourceShortcuts = "[kwin]\n" +
	"_k_friendly_name=KWin\n" +
	"Show Desktop=Meta+D\\tCtrl+F12,Meta+D,Peek at Desktop\n" +
	"Window Close=Alt+F4,Alt+F4,Close Window\n" + // default: not collected
	"Switch\\, Left=Meta+Left,none,Switch\\, Left\n" +
	"\n[services][org.kde.dolphin.desktop]\n_launch=Meta+E\n" +
	"\n[services][org.gone.app.desktop]\n_launch=Meta+G\n" +
	"\n[services][" + "abc-123.desktop]\n_launch=Ctrl+Alt+T\n" +
	"\n[services][nocmd.desktop]\n_launch=Ctrl+Alt+N\n" +
	"\n[org.kde.kate.desktop]\n_k_friendly_name=Kate\n_launch=Meta+K,none,Kate\nnew-window=Ctrl+Shift+K,none,New Window\n"

func shortcutsEnv(t *testing.T) *Env {
	env := newTestEnv(t)
	write(t, filepath.Join(env.ConfigHome, "kglobalshortcutsrc"), sourceShortcuts)
	write(t, filepath.Join(env.DataHome, "applications", "org.kde.dolphin.desktop"), "[Desktop Entry]\nName=Dolphin\n")
	write(t, filepath.Join(env.DataHome, "applications", "org.gone.app.desktop"), "[Desktop Entry]\nName=Gone\n")
	write(t, filepath.Join(env.DataHome, "applications", "org.kde.kate.desktop"), "[Desktop Entry]\nName=Kate\n")
	write(t, filepath.Join(env.DataHome, "kglobalaccel", "abc-123.desktop"),
		"[Desktop Entry]\nName=Terminal\nExec=installed-tool --flag\nX-KDE-GlobalAccel-CommandShortcut=true\n")
	write(t, filepath.Join(env.DataHome, "kglobalaccel", "nocmd.desktop"),
		"[Desktop Entry]\nName=Missing\nExec=env FOO=1 missing-tool \"a b\"\nX-KDE-GlobalAccel-CommandShortcut=true\n")
	return env
}

func TestGlobalShortcutsCollect(t *testing.T) {
	frag, err := Find("hotkeys", "global-shortcuts").Collect(shortcutsEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range frag.Items {
		ids = append(ids, it.ID)
	}
	want := []string{
		"kwin/Show Desktop", "kwin/Switch\\, Left",
		"service/org.kde.dolphin.desktop/_launch", "service/org.gone.app.desktop/_launch",
		"service/org.kde.kate.desktop/_launch", "service/org.kde.kate.desktop/new-window",
	}
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("items:\n%v\nwant\n%v", ids, want)
	}
	if frag.Items[0].Group != "KWin" || !strings.Contains(frag.Items[0].Label, "Peek at Desktop") ||
		!strings.Contains(frag.Items[0].Label, "Meta+D, Ctrl+F12") {
		t.Errorf("item: %+v", frag.Items[0])
	}
	// legacy kate group is carried in the current format, keys only
	for _, kv := range frag.Keys {
		if kv.Item == "service/org.kde.kate.desktop/_launch" && (kv.Group != "services][org.kde.kate.desktop" || kv.Value != "Meta+K") {
			t.Errorf("legacy service not normalised: %+v", kv)
		}
	}
}

func TestGlobalShortcutsApply(t *testing.T) {
	src := shortcutsEnv(t)
	frag, _ := Find("hotkeys", "global-shortcuts").Collect(src)

	dst := newTestEnv(t)
	write(t, filepath.Join(dst.ConfigHome, "kglobalshortcutsrc"),
		"[kwin]\n_k_friendly_name=KWin\nShow Desktop=Meta+D,Meta+D,Show Desktop (target)\n")
	write(t, filepath.Join(dst.DataHome, "applications", "org.kde.dolphin.desktop"), "[Desktop Entry]\nName=Dolphin\n")
	write(t, filepath.Join(dst.DataHome, "applications", "org.kde.kate.desktop"), "[Desktop Entry]\nName=Kate\n")

	rep, err := Find("hotkeys", "global-shortcuts").Apply(dst, frag, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := kconfig.ReadFile(filepath.Join(dst.ConfigHome, "kglobalshortcutsrc"))
	check := func(group, key, want string) {
		t.Helper()
		if v, _ := got.Get(group, key); v != want {
			t.Errorf("[%s] %s = %q, want %q", group, key, v, want)
		}
	}
	// existing component action: only the active keys change
	check("kwin", "Show Desktop", `Meta+D\tCtrl+F12,Meta+D,Show Desktop (target)`)
	// new component action: carried whole
	check("kwin", "Switch\\, Left", "Meta+Left,none,Switch\\, Left")
	check("services][org.kde.dolphin.desktop", "_launch", "Meta+E")
	check("services][org.kde.kate.desktop", "new-window", "Ctrl+Shift+K")
	if _, ok := got.Get("services][org.gone.app.desktop", "_launch"); ok {
		t.Error("shortcut for an app that isn't installed was restored")
	}
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "org.gone.app.desktop") {
		t.Errorf("skipped: %v", rep.Skipped)
	}
}

func TestGlobalShortcutsApplyHonoursSelection(t *testing.T) {
	src := shortcutsEnv(t)
	frag, _ := Find("hotkeys", "global-shortcuts").Collect(src)
	dst := newTestEnv(t)
	write(t, filepath.Join(dst.DataHome, "applications", "org.kde.dolphin.desktop"), "[Desktop Entry]\nName=Dolphin\n")
	if _, err := Find("hotkeys", "global-shortcuts").Apply(dst, frag, nil, Selection{"service/org.kde.dolphin.desktop/_launch": true}); err != nil {
		t.Fatal(err)
	}
	if b := read(t, filepath.Join(dst.ConfigHome, "kglobalshortcutsrc")); b != "[services][org.kde.dolphin.desktop]\n_launch=Meta+E\n" {
		t.Errorf("got:\n%s", b)
	}
}

func TestGlobalShortcutsApplyToPlasma5(t *testing.T) {
	src := shortcutsEnv(t)
	frag, _ := Find("hotkeys", "global-shortcuts").Collect(src)
	dst := newTestEnv(t)
	dst.PlasmaMajor = 5
	write(t, filepath.Join(dst.DataHome, "applications", "org.kde.dolphin.desktop"), "[Desktop Entry]\nName=Dolphin\n")
	if _, err := Find("hotkeys", "global-shortcuts").Apply(dst, frag, nil, Selection{"service/org.kde.dolphin.desktop/_launch": true}); err != nil {
		t.Fatal(err)
	}
	if b := read(t, filepath.Join(dst.ConfigHome, "kglobalshortcutsrc")); b != "[org.kde.dolphin.desktop]\n_launch=Meta+E,none,Dolphin\n" {
		t.Errorf("got:\n%s", b)
	}
}

func TestCustomHotkeys(t *testing.T) {
	src := shortcutsEnv(t)
	sub := Find("hotkeys", "custom-hotkeys")
	frag, err := sub.Collect(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(frag.Items) != 2 || len(frag.Files) != 2 || len(frag.Keys) != 2 {
		t.Fatalf("fragment: %+v", frag)
	}
	if !strings.Contains(frag.Items[0].Label, "Terminal") && !strings.Contains(frag.Items[1].Label, "Terminal") {
		t.Errorf("items: %+v", frag.Items)
	}

	dst := newTestEnv(t)
	// an older copy of one command is updated, not duplicated
	write(t, filepath.Join(dst.DataHome, "kglobalaccel", "abc-123.desktop"), "[Desktop Entry]\nName=Old\n")
	rep, err := sub.Apply(dst, frag, archiveOf(t, src, frag), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b := read(t, filepath.Join(dst.DataHome, "kglobalaccel", "abc-123.desktop")); !strings.Contains(b, "Name=Terminal") {
		t.Errorf("not updated:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dst.DataHome, "kglobalaccel", "nocmd.desktop")); err == nil {
		t.Error("a command whose program isn't installed was restored")
	}
	if b := read(t, filepath.Join(dst.ConfigHome, "kglobalshortcutsrc")); b != "[services][abc-123.desktop]\n_launch=Ctrl+Alt+T\n" {
		t.Errorf("shortcuts:\n%s", b)
	}
	if len(rep.Applied) != 1 || !strings.Contains(rep.Applied[0], "updated") ||
		len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "missing-tool") {
		t.Errorf("report: %+v", rep)
	}
}

func TestMissingProgram(t *testing.T) {
	env := newTestEnv(t)
	cases := map[string]string{
		"installed-tool -x":             "",
		"env A=1 installed-tool":        "",
		`"installed-tool" a b`:          "",
		"bash -c 'whatever | whatever'": "",
		"missing-tool":                  "missing-tool",
		"/nonexistent/bin/tool --x":     "/nonexistent/bin/tool",
		"FOO=bar missing-tool":          "missing-tool",
	}
	for line, want := range cases {
		got, missing := missingProgram(env, line)
		if want == "" && missing || want != "" && (!missing || got != want) {
			t.Errorf("%q: got %q,%v want %q", line, got, missing, want)
		}
	}
}

func TestValidateRejectsEscapes(t *testing.T) {
	bad := []Fragment{
		{Files: []FileRef{{Root: "home", Path: "../.bashrc"}}},
		{Files: []FileRef{{Root: "home", Path: "/etc/passwd"}}},
		{Files: []FileRef{{Root: "etc", Path: "x"}}},
		{Keys: []KeyValue{{File: "../x", Group: "g", Key: "k"}}},
		{Keys: []KeyValue{{File: "/abs", Group: "g", Key: "k"}}},
	}
	for i, f := range bad {
		if f.Validate() == nil {
			t.Errorf("case %d accepted: %+v", i, f)
		}
	}
	ok := Fragment{Files: []FileRef{{Root: "data", Path: "icons/x"}}, Keys: []KeyValue{{File: "kdeglobals", Group: "g", Key: "k"}}}
	if err := ok.Validate(); err != nil {
		t.Error(err)
	}
}
