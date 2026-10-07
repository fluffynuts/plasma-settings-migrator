package components

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fluffynuts/plasma-settings-migrator/internal/kconfig"
)

const (
	shortcutsFile  = "kglobalshortcutsrc"
	servicesPrefix = "services]["
	commandFlag    = "X-KDE-GlobalAccel-CommandShortcut"
)

func hotkeysCategory() *Category {
	return &Category{
		ID:    "hotkeys",
		Label: "Hotkeys",
		Subs: []*SubComponent{
			{ID: "global-shortcuts", Label: "Global shortcuts", Collect: collectGlobalShortcuts, Apply: applyGlobalShortcuts},
			{ID: "custom-hotkeys", Label: "Custom hotkeys", Collect: collectCustomHotkeys, Apply: applyCustomHotkeys},
		},
	}
}

// How kglobalshortcutsrc is laid out (see kglobalacceld):
//
//   - A component such as kwin is a group of its own, each action being
//     "active keys,default keys,friendly name" (keys tab-separated, commas
//     in the text escaped), plus _k_friendly_name for the component.
//   - Since Plasma 6, an application's .desktop file is the group
//     [services][app.desktop], holding only what differs from the file's
//     defaults: "_launch=keys", or an action's name and its keys.
//   - Plasma 5 kept those as [app.desktop] with the triplets of
//     a component. Plasma 6 migrates them on first run.
//   - A "custom command" shortcut is a .desktop file flagged with
//     X-KDE-GlobalAccel-CommandShortcut=true (in ~/.local/share/kglobalaccel
//     when made by Plasma 6), bound as a service like any other.

// binding is one shortcut found in kglobalshortcutsrc.
type binding struct {
	group   string // the group, as written in the file
	key     string // the action
	value   string // as in the file
	keys    string // the active keys alone
	def     string // the default keys; "" for service bindings
	label   string // the action's friendly name, if the file has one
	service string // the .desktop id, if it is a service binding
	comp    string // the component's friendly name
}

func readBindings(env *Env) ([]binding, error) {
	f, err := kconfig.ReadFile(filepath.Join(env.ConfigHome, shortcutsFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []binding
	inNewFormat := map[string]bool{}
	for _, g := range f.Groups() {
		if id, ok := strings.CutPrefix(g.Name, servicesPrefix); ok && !strings.Contains(id, "][") {
			inNewFormat[id] = true
		}
	}
	for _, g := range f.Groups() {
		name := g.Name
		var service string
		legacy := false
		switch {
		case name == "services" || name == "khotkeys" || strings.HasPrefix(name, "$"):
			continue
		case strings.HasPrefix(name, servicesPrefix):
			service = strings.TrimPrefix(name, servicesPrefix)
		case strings.HasSuffix(name, ".desktop"):
			service, legacy = name, true
			if inNewFormat[service] {
				continue
			}
		}
		if strings.Contains(service, "][") || (service == "" && strings.Contains(name, "][")) {
			continue
		}
		comp := name
		if v, ok := g.Get("_k_friendly_name"); ok && v != "" {
			comp = unescapeText(v)
		}
		for _, e := range g.Pairs() {
			if e.Key == "_k_friendly_name" {
				continue
			}
			b := binding{group: name, key: e.Key, value: e.Value, service: service, comp: comp}
			if service != "" && !legacy {
				b.keys = e.Value
			} else {
				parts := splitUnescaped(e.Value, ',')
				b.keys = parts[0]
				if len(parts) == 3 {
					b.def, b.label = parts[1], unescapeText(parts[2])
				}
			}
			out = append(out, b)
		}
	}
	return out, nil
}

func unescapeText(s string) string { return strings.ReplaceAll(s, `\,`, ",") }

func displayKeys(keys string) string {
	if keys == "" || keys == "none" {
		return "unbound"
	}
	// Alternates are tab-separated, and KConfig writes the tab as "\t".
	return strings.NewReplacer(`\t`, ", ", "\t", ", ").Replace(keys)
}

// desktopFile finds an application's .desktop file: the user's own
// (including those Plasma keeps for custom commands) first, then the
// system's.
func desktopFile(env *Env, id string) (string, *kconfig.File) {
	dirs := []string{filepath.Join(env.DataHome, "kglobalaccel"), filepath.Join(env.DataHome, "applications")}
	for _, d := range env.DataDirs {
		dirs = append(dirs, filepath.Join(d, "applications"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, id)
		if f, err := kconfig.ReadFile(p); err == nil {
			return p, f
		}
	}
	return "", nil
}

func isCommandShortcut(f *kconfig.File) bool {
	if f == nil {
		return false
	}
	v, _ := f.Get("Desktop Entry", commandFlag)
	return strings.EqualFold(v, "true")
}

func appName(f *kconfig.File, fallback string) string {
	if f != nil {
		if n, _ := f.Get("Desktop Entry", "Name"); n != "" {
			return n
		}
	}
	return fallback
}

func collectGlobalShortcuts(env *Env) (*Fragment, error) {
	bindings, err := readBindings(env)
	if err != nil {
		return nil, err
	}
	frag := &Fragment{}
	for _, b := range bindings {
		kv := KeyValue{File: shortcutsFile, Group: b.group, Key: b.key, Value: b.value}
		var item Item
		if b.service != "" {
			_, df := desktopFile(env, b.service)
			if isCommandShortcut(df) {
				continue // a custom hotkey: it has a sub-component of its own
			}
			name := appName(df, strings.TrimSuffix(b.service, ".desktop"))
			what := "Launch " + name
			if b.key != "_launch" {
				what = name + ": " + b.key
			}
			item = Item{ID: "service/" + b.service + "/" + b.key, Label: what + "  [" + displayKeys(b.keys) + "]", Group: "Applications"}
			// A service binding in either format is carried in the
			// current one, with just the keys; applying converts back
			// for Plasma 5.
			kv.Group, kv.Value = servicesPrefix+b.service, b.keys
		} else {
			if b.def != "" && b.keys == b.def {
				continue // not customised: the target has it already
			}
			label := b.label
			if label == "" {
				label = b.key
			}
			item = Item{ID: b.group + "/" + b.key, Label: label + "  [" + displayKeys(b.keys) + "]", Group: b.comp}
		}
		kv.Item = item.ID
		frag.Items = append(frag.Items, item)
		frag.Keys = append(frag.Keys, kv)
	}
	return frag, nil
}

func applyGlobalShortcuts(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error) {
	return applyShortcutKeys(env, f, sel, nil)
}

// applyShortcutKeys writes the selected keys of f into kglobalshortcutsrc.
// Bindings of applications are only written for those installed here.
// Items in allow, when given, are the only ones considered.
func applyShortcutKeys(env *Env, f *Fragment, sel Selection, allow map[string]bool) (*Report, error) {
	rep := &Report{}
	labels := map[string]string{}
	for _, it := range f.Items {
		labels[it.ID] = it.Label
	}
	var kvs []KeyValue
	for _, kv := range f.Keys {
		if !sel.Has(kv.Item) || (allow != nil && !allow[kv.Item]) {
			continue
		}
		if id, ok := strings.CutPrefix(kv.Group, servicesPrefix); ok {
			if _, df := desktopFile(env, id); df == nil {
				rep.skipped("%s: %s isn't installed", labelOr(labels, kv.Item, kv.Key), id)
				continue
			}
			if !env.plasma6() {
				kv.Group = id // Plasma 5 keeps these as components
			}
		}
		kvs = append(kvs, kv)
	}
	written, _, err := writeKeys(env, kvs, func(kv KeyValue, existing *string) (string, string) {
		isService := strings.HasSuffix(kv.Group, ".desktop")
		triplet := !env.plasma6() || !isService
		if !triplet {
			return kv.Value, ""
		}
		// Triplet: only the active keys are ours to set; the default keys
		// and friendly name belong to the machine the keys are going to.
		keys := kv.Value
		if parts := splitUnescaped(kv.Value, ','); len(parts) == 3 {
			keys = parts[0]
		}
		if existing != nil {
			if parts := splitUnescaped(*existing, ','); len(parts) == 3 {
				return keys + "," + parts[1] + "," + parts[2], ""
			}
		}
		if strings.HasPrefix(kv.Value, keys+",") && len(splitUnescaped(kv.Value, ',')) == 3 {
			return kv.Value, ""
		}
		_, df := desktopFile(env, kv.Group)
		return keys + ",none," + strings.ReplaceAll(appName(df, kv.Group), ",", `\,`), ""
	})
	if err != nil {
		return rep, err
	}
	for _, kv := range written {
		rep.applied("%s", labelOr(labels, kv.Item, kv.Key))
	}
	return rep, nil
}

func labelOr(labels map[string]string, id, fallback string) string {
	if l := labels[id]; l != "" {
		return l
	}
	return fallback
}

func collectCustomHotkeys(env *Env) (*Fragment, error) {
	bindings, err := readBindings(env)
	if err != nil {
		return nil, err
	}
	keysOf := map[string]binding{}
	for _, b := range bindings {
		if b.service != "" && b.key == "_launch" {
			keysOf[b.service] = b
		}
	}
	frag := &Fragment{}
	for _, dir := range []string{"kglobalaccel", "applications"} {
		entries, err := os.ReadDir(filepath.Join(env.DataHome, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			id := e.Name()
			if e.IsDir() || !strings.HasSuffix(id, ".desktop") {
				continue
			}
			b, bound := keysOf[id]
			df, err := kconfig.ReadFile(filepath.Join(env.DataHome, dir, id))
			if err != nil || !isCommandShortcut(df) || !bound {
				continue
			}
			exec, _ := df.Get("Desktop Entry", "Exec")
			item := Item{
				ID:    "custom/" + id,
				Label: appName(df, id) + "  [" + displayKeys(b.keys) + "]  " + exec,
			}
			frag.Items = append(frag.Items, item)
			frag.Files = append(frag.Files, FileRef{Item: item.ID, Root: "data", Path: dir + "/" + id})
			frag.Keys = append(frag.Keys, KeyValue{Item: item.ID, File: shortcutsFile, Group: servicesPrefix + id, Key: "_launch", Value: b.keys})
		}
	}
	sort.SliceStable(frag.Items, func(i, j int) bool { return frag.Items[i].Label < frag.Items[j].Label })
	return frag, nil
}

// applyCustomHotkeys installs each selected command's .desktop file, a
// new one or over an existing one of the same name, unless the program it
// runs isn't here, and binds its shortcut.
func applyCustomHotkeys(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error) {
	rep := &Report{}
	allow := map[string]bool{}
	for _, ref := range f.Files {
		if !sel.Has(ref.Item) {
			continue
		}
		label := ref.Path
		for _, it := range f.Items {
			if it.ID == ref.Item {
				label = it.Label
			}
		}
		df, err := readArchived(src, path.Join(ArchiveFilesDir, ref.Root, ref.Path))
		if err != nil {
			return rep, err
		}
		exec, _ := df.Get("Desktop Entry", "Exec")
		if prog, ok := missingProgram(env, exec); ok {
			rep.skipped("%s: %s isn't installed", label, prog)
			continue
		}
		verb := "created"
		if _, err := os.Stat(env.FilePath(ref)); err == nil {
			verb = "updated"
		}
		if err := restoreFile(env, src, ref); err != nil {
			return rep, err
		}
		allow[ref.Item] = true
		rep.applied("%s (%s)", label, verb)
	}
	sub, err := applyShortcutKeys(env, f, sel, allow)
	if err != nil {
		return rep, err
	}
	rep.Skipped = append(rep.Skipped, sub.Skipped...)
	return rep, nil
}

func readArchived(src fs.FS, name string) (*kconfig.File, error) {
	fh, err := src.Open(name)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return kconfig.Parse(fh)
}

// missingProgram reports the program an Exec line starts, when it can't be
// found. Lines that go through a shell are given the benefit of the doubt.
func missingProgram(env *Env, execLine string) (string, bool) {
	words := shellWords(execLine)
	for len(words) > 0 && (words[0] == "env" || strings.Contains(words[0], "=")) {
		words = words[1:]
	}
	if len(words) == 0 {
		return "", false
	}
	prog := words[0]
	switch filepath.Base(prog) {
	case "sh", "bash", "zsh", "fish", "dash":
		return "", false
	}
	if strings.Contains(prog, "/") {
		info, err := os.Stat(prog)
		return prog, err != nil || info.IsDir()
	}
	_, err := env.LookPath(prog)
	return prog, err != nil
}

// shellWords splits a command line on spaces, honouring quotes and
// backslashes well enough to find the program.
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	var quote rune
	in := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, in = r, true
		case r == ' ' || r == '\t':
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		words = append(words, cur.String())
	}
	return words
}
