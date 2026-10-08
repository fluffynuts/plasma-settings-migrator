package components

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func themeCategory() *Category {
	// In this order, so a restore applies the global theme first and the
	// parts the user changed from it after.
	return &Category{
		ID:    "theme",
		Label: "Theme",
		Subs: []*SubComponent{
			{ID: "global-theme", Label: "Global theme", Collect: collectGlobalTheme, Apply: applyGlobalTheme},
			{ID: "plasma-style", Label: "Plasma style", Collect: collectPlasmaStyle, Apply: applyPlasmaStyle},
			{ID: "color-scheme", Label: "Color scheme", Collect: collectColorScheme, Apply: applyColorScheme},
			{ID: "icon-theme", Label: "Icon theme", Collect: collectIconTheme, Apply: notifying(applyGeneric, iconsChanged)},
			{ID: "fonts", Label: "Fonts", Collect: collectFonts, Apply: notifying(applyGeneric, changed(fontChanged))},
			{ID: "widget-style", Label: "Widget style", Collect: collectWidgetStyle, Apply: notifying(applyGeneric, changed(styleChanged, toolbarStyleChanged))},
			{ID: "gtk-style", Label: "Gtk style", Collect: collectGtk, Apply: applyGeneric},
		},
	}
}

// What Plasma does at login (startplasma.cpp, Plasma 5.27 and 6):
//
//   - If kdedefaults/package doesn't name the global theme in kdeglobals
//     [KDE] LookAndFeelPackage, the theme's settings are written to
//     ~/.config/kdedefaults/, under the user's own files.
//   - If [General] ColorSchemeHash isn't the SHA-1 of the file of the
//     scheme [General] ColorScheme names (BreezeLight when unset), that
//     scheme is applied again, over whatever colours kdeglobals has.
//
// So the colours alone don't survive a login: the scheme's name has to
// come along, and the hash must make Plasma re-apply that scheme (from
// this machine's copy of it) or leave the colours be.
//
// The plasma-apply-* tools do nothing when the setting already names what
// they're asked for, so in the running session they're run before the
// name is written, not after.

const (
	lookAndFeelDir      = "plasma/look-and-feel/"
	desktopThemeDir     = "plasma/desktoptheme/"
	colorSchemesDir     = "color-schemes/"
	defaultScheme       = "BreezeLight"
	kglobalsettings     = "/KGlobalSettings"
	notifyChange        = "org.kde.KGlobalSettings.notifyChange"
	paletteChanged      = 0 // KGlobalSettings::ChangeType
	fontChanged         = 1
	styleChanged        = 2
	iconChanged         = 4
	toolbarStyleChanged = 6
)

func collectGlobalTheme(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "", KeySpec{File: "kdeglobals", Group: "KDE", Keys: []string{"LookAndFeelPackage"}})
	if err != nil {
		return nil, err
	}
	f := &Fragment{Keys: keys}
	if name := valueOf(keys, "KDE", "LookAndFeelPackage"); name != "" {
		f.Files = existingFiles(env, FileRef{Root: "data", Path: lookAndFeelDir + name})
	}
	return f, nil
}

func applyGlobalTheme(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error) {
	rep := &Report{}
	if err := restoreFiles(env, src, f, sel, rep); err != nil {
		return rep, err
	}
	name := valueOf(selectedKeys(f, sel), "KDE", "LookAndFeelPackage")
	if name == "" {
		return rep, nil
	}
	if env.findData(lookAndFeelDir+name) == "" {
		rep.skipped("global theme %s isn't installed here", name)
		return rep, nil
	}
	if env.ApplyNow {
		for _, tool := range []string{"plasma-apply-lookandfeel", "lookandfeeltool"} {
			if env.have(tool) {
				if err := env.run(tool, "--apply", name); err == nil {
					rep.applied("global theme %s, in the running session", name)
					return rep, nil
				}
				rep.skipped("%s couldn't apply %s now, so it is set for your next login", tool, name)
				break
			}
		}
	}
	// Plasma applies it at login, seeing kdedefaults/package doesn't match.
	if _, _, err := writeKeys(env, selectedKeys(f, sel), nil); err != nil {
		return rep, err
	}
	rep.applied("global theme %s, from your next login", name)
	return rep, nil
}

func collectPlasmaStyle(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "", KeySpec{File: "plasmarc", Group: "Theme", Keys: []string{"name"}})
	if err != nil {
		return nil, err
	}
	f := &Fragment{Keys: keys}
	if name := valueOf(keys, "Theme", "name"); name != "" {
		f.Files = existingFiles(env, FileRef{Root: "data", Path: desktopThemeDir + name})
	}
	return f, nil
}

func applyPlasmaStyle(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error) {
	rep := &Report{}
	if err := restoreFiles(env, src, f, sel, rep); err != nil {
		return rep, err
	}
	keys := selectedKeys(f, sel)
	name := valueOf(keys, "Theme", "name")
	if name == "" {
		return rep, nil
	}
	if env.findData(desktopThemeDir+name) == "" {
		rep.skipped("Plasma style %s isn't installed here", name)
		return rep, nil
	}
	if env.ApplyNow && env.have("plasma-apply-desktoptheme") {
		if err := env.run("plasma-apply-desktoptheme", name); err == nil {
			rep.applied("Plasma style %s, in the running session", name)
			return rep, nil
		}
		rep.skipped("plasma-apply-desktoptheme couldn't apply %s now, so it is set for your next login", name)
	}
	if _, _, err := writeKeys(env, keys, nil); err != nil {
		return rep, err
	}
	rep.applied("Plasma style %s, from your next login", name)
	return rep, nil
}

// The color scheme is spread over several groups of kdeglobals: the
// colour sets themselves, the effects for inactive and disabled widgets,
// the window decoration's title bar colours, and a few General and KDE
// keys. (Keys as written by plasma-workspace's colors KCM.)
func collectColorScheme(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "",
		KeySpec{File: "kdeglobals", Group: "Colors:*"},
		KeySpec{File: "kdeglobals", Group: "ColorEffects:*"},
		KeySpec{File: "kdeglobals", Group: "WM", Keys: []string{
			"activeBackground", "activeForeground", "inactiveBackground", "inactiveForeground",
			"activeBlend", "inactiveBlend",
		}},
		KeySpec{File: "kdeglobals", Group: "General", Keys: accentKeys},
		KeySpec{File: "kdeglobals", Group: "General", Keys: []string{"ColorScheme"}},
		KeySpec{File: "kdeglobals", Group: "KDE", Keys: []string{"contrast", "frameContrast"}},
	)
	if err != nil || len(keys) == 0 {
		return &Fragment{}, err
	}
	name := valueOf(keys, "General", "ColorScheme")
	if name == "" {
		// Not set anywhere: then it is the scheme whose file the hash
		// Plasma keeps is of.
		hash := effective(env, "kdeglobals", "General", "ColorSchemeHash", "")
		if name = schemeWithHash(env, hash); name != "" {
			keys = append(keys, KeyValue{File: "kdeglobals", Group: "General", Key: "ColorScheme", Value: name})
		}
	}
	f := &Fragment{Keys: keys}
	// A scheme the user installed is a file of its own, which the target
	// needs for the name above to resolve.
	if name != "" {
		f.Files = existingFiles(env, FileRef{Root: "data", Path: colorSchemesDir + name + ".colors"})
	}
	return f, nil
}

var accentKeys = []string{"AccentColor", "LastUsedCustomAccentColor", "accentColorFromWallpaper"}

func applyColorScheme(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error) {
	rep := &Report{}
	if err := restoreFiles(env, src, f, sel, rep); err != nil {
		return rep, err
	}
	var keys []KeyValue
	for _, kv := range selectedKeys(f, sel) {
		if kv.Group == "General" && kv.Key == "ColorSchemeHash" {
			continue // older backups carry it: it is worked out below instead
		}
		keys = append(keys, kv)
	}
	if len(keys) == 0 {
		return rep, nil
	}
	name := valueOf(keys, "General", "ColorScheme")
	file := ""
	if name != "" {
		file = env.findData(colorSchemesDir + name + ".colors")
	}

	if env.ApplyNow && file != "" && env.have("plasma-apply-colorscheme") {
		var accent []KeyValue
		for _, kv := range keys {
			if kv.Group == "General" && contains(accentKeys, kv.Key) {
				accent = append(accent, kv)
			}
		}
		if _, _, err := writeKeys(env, accent, nil); err != nil {
			return rep, err
		}
		if err := env.run("plasma-apply-colorscheme", name); err == nil {
			rep.applied("color scheme %s, in the running session", name)
			return rep, nil
		}
		rep.skipped("plasma-apply-colorscheme couldn't apply %s, so its colours were written instead", name)
	}

	// The hash decides what Plasma does at login (see above).
	hash := "" // re-apply the named scheme from this machine's file
	if file == "" {
		// The colours have no scheme here to come from: make the hash
		// match the scheme Plasma would otherwise re-apply over them.
		current := name
		if current == "" {
			current = effective(env, "kdeglobals", "General", "ColorScheme", defaultScheme)
		}
		hash = fileSHA1(env.findData(colorSchemesDir + current + ".colors"))
	}
	keys = append(keys, KeyValue{File: "kdeglobals", Group: "General", Key: "ColorSchemeHash", Value: hash})
	if _, _, err := writeKeys(env, keys, nil); err != nil {
		return rep, err
	}
	what := "color scheme " + name
	if name == "" {
		what = "colours (the backup doesn't name their scheme)"
	}
	if env.ApplyNow {
		env.notify(kglobalsettings, notifyChange, "int32:0", "int32:0")
		rep.applied("%s, for applications started from now on, and everywhere from your next login", what)
	} else {
		rep.applied("%s, from your next login", what)
	}
	return rep, nil
}

// schemeWithHash finds the installed color scheme whose file has the hash
// Plasma keeps in ColorSchemeHash.
func schemeWithHash(env *Env, hash string) string {
	if hash == "" {
		return ""
	}
	for _, d := range append([]string{env.DataHome}, env.DataDirs...) {
		matches, _ := filepath.Glob(filepath.Join(d, colorSchemesDir, "*.colors"))
		for _, m := range matches {
			if fileSHA1(m) == hash {
				return strings.TrimSuffix(filepath.Base(m), ".colors")
			}
		}
	}
	return ""
}

func fileSHA1(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func collectIconTheme(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "", KeySpec{File: "kdeglobals", Group: "Icons", Keys: []string{"Theme"}})
	if err != nil {
		return nil, err
	}
	f := &Fragment{Keys: keys}
	if name := valueOf(keys, "Icons", "Theme"); name != "" {
		f.Files = existingFiles(env, FileRef{Root: "data", Path: "icons/" + name})
	}
	return f, nil
}

func collectFonts(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "",
		KeySpec{File: "kdeglobals", Group: "General", Keys: []string{
			"font", "fixed", "smallestReadableFont", "toolBarFont", "menuFont",
		}},
		KeySpec{File: "kdeglobals", Group: "WM", Keys: []string{"activeFont"}},
	)
	return &Fragment{Keys: keys}, err
}

func collectWidgetStyle(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "",
		KeySpec{File: "kdeglobals", Group: "KDE", Keys: []string{
			"widgetStyle", "unionStyle", "ShowIconsOnPushButtons", "ShowIconsInMenuItems",
		}},
		KeySpec{File: "kdeglobals", Group: "Toolbar style", Keys: []string{
			"ToolButtonStyle", "ToolButtonStyleOtherToolbars",
		}},
	)
	return &Fragment{Keys: keys}, err
}

func collectGtk(env *Env) (*Fragment, error) {
	return &Fragment{Files: existingFiles(env,
		FileRef{Root: "config", Path: "gtk-3.0/settings.ini"},
		FileRef{Root: "config", Path: "gtk-4.0/settings.ini"},
		FileRef{Root: "home", Path: ".gtkrc-2.0"},
	)}, nil
}

// notifying wraps an Apply so that, when the change is wanted in the
// running session and something was written, running programs are told.
func notifying(apply func(*Env, *Fragment, fs.FS, Selection) (*Report, error), tell func(*Env)) func(*Env, *Fragment, fs.FS, Selection) (*Report, error) {
	return func(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error) {
		rep, err := apply(env, f, src, sel)
		if err == nil && env.ApplyNow && len(rep.Applied) > 0 {
			tell(env)
			rep.applied("running applications were told; some only show it once restarted")
		}
		return rep, err
	}
}

func changed(types ...int) func(*Env) {
	return func(env *Env) {
		for _, t := range types {
			env.notify(kglobalsettings, notifyChange, sprintf("int32:%d", t), "int32:0")
		}
	}
}

// iconsChanged does what Plasma's icons settings do: drop the icon cache,
// and tell every icon group it changed.
func iconsChanged(env *Env) {
	if env.CacheHome != "" {
		os.Remove(filepath.Join(env.CacheHome, "icon-cache.kcache"))
	}
	for group := 0; group < 6; group++ { // KIconLoader::Desktop .. Dialog
		env.notify("/KIconLoader", "org.kde.KIconLoader.iconChanged", sprintf("int32:%d", group))
	}
	changed(iconChanged)(env)
}

func selectedKeys(f *Fragment, sel Selection) []KeyValue {
	var out []KeyValue
	for _, kv := range f.Keys {
		if sel.Has(kv.Item) {
			out = append(out, kv)
		}
	}
	return out
}

// restoreFiles restores the fragment's selected files.
func restoreFiles(env *Env, src fs.FS, f *Fragment, sel Selection, rep *Report) error {
	for _, ref := range f.Files {
		if !sel.Has(ref.Item) {
			continue
		}
		if err := restoreFile(env, src, ref); err != nil {
			return err
		}
		rep.applied("%s/%s", ref.Root, ref.Path)
	}
	return nil
}

func valueOf(keys []KeyValue, group, key string) string {
	for _, kv := range keys {
		if kv.Group == group && kv.Key == key {
			return kv.Value
		}
	}
	return ""
}
