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

// ran records the programs an Env runs.
func ran(env *Env) *[]string {
	var cmds []string
	env.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	env.Run = func(name string, args ...string) error {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return nil
	}
	return &cmds
}

func TestThemeReadsWhatTheGlobalThemeSet(t *testing.T) {
	// Plasma writes a global theme's settings to kdedefaults: those the user
	// didn't change are only there.
	src := newTestEnv(t)
	write(t, filepath.Join(src.ConfigHome, "kdeglobals"), "[Colors:Window]\nBackgroundNormal=49,54,59\n\n[Icons]\nTheme=Papirus\n")
	write(t, filepath.Join(src.ConfigHome, "kdedefaults", "kdeglobals"),
		"[General]\nColorScheme=BreezeDark\n\n[Icons]\nTheme=breeze-dark\n\n[KDE]\nLookAndFeelPackage=org.kde.breezedark.desktop\n")
	write(t, filepath.Join(src.ConfigHome, "kdedefaults", "plasmarc"), "[Theme]\nname=breeze-dark\n")
	for _, c := range []struct{ sub, group, key, want string }{
		{"color-scheme", "General", "ColorScheme", "BreezeDark"},
		{"color-scheme", "Colors:Window", "BackgroundNormal", "49,54,59"},
		{"icon-theme", "Icons", "Theme", "Papirus"}, // the user's own wins
		{"global-theme", "KDE", "LookAndFeelPackage", "org.kde.breezedark.desktop"},
		{"plasma-style", "Theme", "name", "breeze-dark"},
	} {
		frag, err := Find("theme", c.sub).Collect(src)
		if err != nil {
			t.Fatal(err)
		}
		if got := valueOf(frag.Keys, c.group, c.key); got != c.want {
			t.Errorf("%s: [%s] %s = %q, want %q", c.sub, c.group, c.key, got, c.want)
		}
	}
}

func TestColorSchemeNamedFromItsHash(t *testing.T) {
	src := newTestEnv(t)
	scheme := filepath.Join(src.DataDirs[0], "color-schemes", "BreezeDark.colors")
	write(t, scheme, "[Colors:Window]\nBackgroundNormal=49,54,59\n")
	write(t, filepath.Join(src.ConfigHome, "kdeglobals"),
		"[Colors:Window]\nBackgroundNormal=49,54,59\n\n[General]\nColorSchemeHash="+fileSHA1(scheme)+"\n")
	frag, _ := Find("theme", "color-scheme").Collect(src)
	if got := valueOf(frag.Keys, "General", "ColorScheme"); got != "BreezeDark" {
		t.Errorf("ColorScheme = %q", got)
	}
	if got := valueOf(frag.Keys, "General", "ColorSchemeHash"); got != "" {
		t.Errorf("hash carried: %q", got)
	}
}

func colorFragment(name string) *Fragment {
	f := &Fragment{Keys: []KeyValue{
		{File: "kdeglobals", Group: "Colors:Window", Key: "BackgroundNormal", Value: "49,54,59"},
		{File: "kdeglobals", Group: "General", Key: "AccentColor", Value: "1,2,3"},
		{File: "kdeglobals", Group: "General", Key: "ColorSchemeHash", Value: "from-the-source"},
	}}
	if name != "" {
		f.Keys = append(f.Keys, KeyValue{File: "kdeglobals", Group: "General", Key: "ColorScheme", Value: name})
	}
	return f
}

func TestColorSchemeForNextLogin(t *testing.T) {
	dst := newTestEnv(t)
	cmds := ran(dst)
	write(t, filepath.Join(dst.DataDirs[0], "color-schemes", "BreezeDark.colors"), "[Colors:Window]\n")
	if _, err := Find("theme", "color-scheme").Apply(dst, colorFragment("BreezeDark"), nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := kconfig.ReadFile(filepath.Join(dst.ConfigHome, "kdeglobals"))
	// an empty hash makes Plasma apply this machine's BreezeDark at login
	for key, want := range map[string]string{"ColorScheme": "BreezeDark", "ColorSchemeHash": "", "AccentColor": "1,2,3"} {
		if v, ok := got.Get("General", key); !ok || v != want {
			t.Errorf("%s = %q (%v), want %q", key, v, ok, want)
		}
	}
	if len(*cmds) != 0 {
		t.Errorf("ran %v", *cmds)
	}
}

func TestUnnamedColorsAreKeptAtLogin(t *testing.T) {
	// No name, so the hash is made to match the scheme Plasma would apply
	// over them, and it leaves them alone.
	dst := newTestEnv(t)
	light := filepath.Join(dst.DataDirs[0], "color-schemes", "BreezeLight.colors")
	write(t, light, "[Colors:Window]\nBackgroundNormal=239,240,241\n")
	if _, err := Find("theme", "color-scheme").Apply(dst, colorFragment(""), nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := kconfig.ReadFile(filepath.Join(dst.ConfigHome, "kdeglobals"))
	if v, _ := got.Get("General", "ColorSchemeHash"); v != fileSHA1(light) {
		t.Errorf("hash = %q", v)
	}
	if v, _ := got.Get("Colors:Window", "BackgroundNormal"); v != "49,54,59" {
		t.Errorf("colours not written: %q", v)
	}
}

func TestColorSchemeNowUsesPlasmasTool(t *testing.T) {
	dst := newTestEnv(t)
	dst.ApplyNow = true
	cmds := ran(dst)
	write(t, filepath.Join(dst.DataDirs[0], "color-schemes", "BreezeDark.colors"), "[Colors:Window]\n")
	rep, err := Find("theme", "color-scheme").Apply(dst, colorFragment("BreezeDark"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 1 || (*cmds)[0] != "plasma-apply-colorscheme BreezeDark" {
		t.Errorf("ran %v", *cmds)
	}
	// the name isn't written first, or the tool would think it already set
	got, _ := kconfig.ReadFile(filepath.Join(dst.ConfigHome, "kdeglobals"))
	if _, ok := got.Get("General", "ColorScheme"); ok {
		t.Error("ColorScheme written before the tool ran")
	}
	if v, _ := got.Get("General", "AccentColor"); v != "1,2,3" {
		t.Errorf("accent = %q", v)
	}
	if !strings.Contains(strings.Join(rep.Applied, "\n"), "running session") {
		t.Errorf("report: %+v", rep)
	}
}

func TestGlobalTheme(t *testing.T) {
	frag := &Fragment{Keys: []KeyValue{{File: "kdeglobals", Group: "KDE", Key: "LookAndFeelPackage", Value: "org.kde.breezedark.desktop"}}}
	sub := Find("theme", "global-theme")

	missing := newTestEnv(t)
	if rep, _ := sub.Apply(missing, frag, nil, nil); len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "isn't installed") {
		t.Errorf("not installed: %+v", rep)
	}

	login := newTestEnv(t)
	cmds := ran(login)
	write(t, filepath.Join(login.DataDirs[0], "plasma", "look-and-feel", "org.kde.breezedark.desktop", "metadata.json"), "{}")
	if _, err := sub.Apply(login, frag, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 0 || !strings.Contains(read(t, filepath.Join(login.ConfigHome, "kdeglobals")), "LookAndFeelPackage=org.kde.breezedark.desktop") {
		t.Errorf("ran %v", *cmds)
	}

	now := newTestEnv(t)
	now.ApplyNow = true
	cmds = ran(now)
	write(t, filepath.Join(now.DataDirs[0], "plasma", "look-and-feel", "org.kde.breezedark.desktop", "metadata.json"), "{}")
	if _, err := sub.Apply(now, frag, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 1 || (*cmds)[0] != "plasma-apply-lookandfeel --apply org.kde.breezedark.desktop" {
		t.Errorf("ran %v", *cmds)
	}
}

func TestIconsNowTellRunningApps(t *testing.T) {
	src, dst := newTestEnv(t), newTestEnv(t)
	write(t, filepath.Join(src.ConfigHome, "kdeglobals"), "[Icons]\nTheme=breeze-dark\n")
	dst.ApplyNow = true
	dst.CacheHome = filepath.Join(dst.Home, ".cache")
	write(t, filepath.Join(dst.CacheHome, "icon-cache.kcache"), "old")
	cmds := ran(dst)
	sub := Find("theme", "icon-theme")
	frag, _ := sub.Collect(src)
	if _, err := sub.Apply(dst, frag, archiveOf(t, src, frag), nil); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(*cmds, "\n")
	if !strings.Contains(all, "/KIconLoader org.kde.KIconLoader.iconChanged int32:0") ||
		!strings.Contains(all, "/KGlobalSettings org.kde.KGlobalSettings.notifyChange int32:4 int32:0") {
		t.Errorf("ran:\n%s", all)
	}
	if _, err := os.Stat(filepath.Join(dst.CacheHome, "icon-cache.kcache")); err == nil {
		t.Error("icon cache kept")
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

// Plasma 5.27 keeps application shortcuts in [services] groups too.
func TestGlobalShortcutsApplyToPlasma5UsesServices(t *testing.T) {
	src := shortcutsEnv(t)
	frag, _ := Find("hotkeys", "global-shortcuts").Collect(src)
	dst := newTestEnv(t)
	dst.PlasmaMajor = 5
	write(t, filepath.Join(dst.DataHome, "applications", "org.kde.dolphin.desktop"), "[Desktop Entry]\nName=Dolphin\n")
	if _, err := Find("hotkeys", "global-shortcuts").Apply(dst, frag, nil, Selection{"service/org.kde.dolphin.desktop/_launch": true}); err != nil {
		t.Fatal(err)
	}
	if b := read(t, filepath.Join(dst.ConfigHome, "kglobalshortcutsrc")); b != "[services][org.kde.dolphin.desktop]\n_launch=Meta+E\n" {
		t.Errorf("got:\n%s", b)
	}
}

func TestUnboundIsUnboundWhetherNoneOrEmpty(t *testing.T) {
	env := newTestEnv(t)
	write(t, filepath.Join(env.ConfigHome, "kglobalshortcutsrc"),
		"[kwin]\nSwitch to Desktop 5=none,,Switch to Desktop 5\nGrid View=none,Meta+G,Toggle Grid View\n")
	frag, err := Find("hotkeys", "global-shortcuts").Collect(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(frag.Items) != 1 || frag.Items[0].ID != "kwin/Grid View" {
		t.Errorf("items: %+v", frag.Items)
	}
}

func TestShortcutsAtLoginWaitForTheLoginHook(t *testing.T) {
	src := shortcutsEnv(t)
	frag, _ := Find("hotkeys", "global-shortcuts").Collect(src)
	dst := newTestEnv(t)
	dst.ShortcutsAtLogin = true
	before := "[kwin]\n_k_friendly_name=KWin\nShow Desktop=Meta+D,Meta+D,Show Desktop (target)\n"
	write(t, filepath.Join(dst.ConfigHome, "kglobalshortcutsrc"), before)
	write(t, filepath.Join(dst.DataHome, "applications", "org.kde.dolphin.desktop"), "[Desktop Entry]\nName=Dolphin\n")

	sel := Selection{"kwin/Show Desktop": true, "service/org.kde.dolphin.desktop/_launch": true}
	rep, err := Find("hotkeys", "global-shortcuts").Apply(dst, frag, nil, sel)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dst.ConfigHome, "kglobalshortcutsrc")); got != before {
		t.Errorf("written now:\n%s", got)
	}
	if !HasPendingShortcuts(dst) || len(rep.Applied) != 1 || !strings.Contains(rep.Applied[0], "next login") {
		t.Fatalf("pending %v, report %+v", HasPendingShortcuts(dst), rep)
	}

	program := filepath.Join(t.TempDir(), "prog")
	write(t, program, "PROGRAM")
	if err := InstallLoginHook(dst, program); err != nil {
		t.Fatal(err)
	}
	script := read(t, hookPath(dst))
	copied := filepath.Join(PendingDir(dst), "plasma-settings-migrator")
	if !strings.Contains(script, "'"+copied+"' apply-pending >>") || !strings.Contains(script, "rm -f '"+hookPath(dst)+"'") {
		t.Errorf("hook script:\n%s", script)
	}
	if read(t, copied) != "PROGRAM" {
		t.Error("program not copied")
	}

	// at login
	rep, err = ApplyPendingShortcuts(dst)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := kconfig.ReadFile(filepath.Join(dst.ConfigHome, "kglobalshortcutsrc"))
	if v, _ := got.Get("kwin", "Show Desktop"); v != `Meta+D\tCtrl+F12,Meta+D,Show Desktop (target)` {
		t.Errorf("Show Desktop = %q", v)
	}
	if v, _ := got.Get("services][org.kde.dolphin.desktop", "_launch"); v != "Meta+E" {
		t.Errorf("dolphin = %q", v)
	}
	if len(rep.Applied) != 2 || !strings.Contains(rep.Applied[0], "Peek at Desktop") {
		t.Errorf("report: %+v", rep)
	}
	for _, p := range []string{hookPath(dst), PendingDir(dst)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s left behind", p)
		}
	}
}

func TestPendingShortcutsAddUp(t *testing.T) {
	env := newTestEnv(t)
	kv := func(key, value string) KeyValue {
		return KeyValue{Item: key, File: "kglobalshortcutsrc", Group: "kwin", Key: key, Value: value}
	}
	deferShortcuts(env, &Fragment{Keys: []KeyValue{kv("a", "1,,A"), kv("b", "2,,B")}})
	deferShortcuts(env, &Fragment{Keys: []KeyValue{kv("b", "3,,B")}})
	ApplyPendingShortcuts(env)
	if b := read(t, filepath.Join(env.ConfigHome, "kglobalshortcutsrc")); b != "[kwin]\na=1,,A\nb=3,,B\n" {
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

func TestPlasmaVersionFromTheSessionFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "wayland-sessions", "gnome.desktop"),
		"[Desktop Entry]\nExec=gnome-session\nX-KDE-PluginInfo-Version=46\n")
	write(t, filepath.Join(dir, "b", "xsessions", "plasmax11.desktop"),
		"[Desktop Entry]\nExec=/usr/lib/plasma-dbus-run-session-if-needed /usr/bin/startplasma-x11\nX-KDE-PluginInfo-Version=6.2.4\n")
	if v := sessionFileVersion([]string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}); v != "6.2.4" {
		t.Errorf("version %q", v)
	}
	if v := sessionFileVersion([]string{filepath.Join(dir, "a")}); v != "" {
		t.Errorf("took another desktop's version: %q", v)
	}
}
