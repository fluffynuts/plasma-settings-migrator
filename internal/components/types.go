// Package components describes what the migrator can back up and restore:
// categories (Theme, Hotkeys) made of sub-components (Color scheme, Global
// shortcuts, ...), each of which knows how to collect its settings from a
// machine into a Fragment, and how to apply a Fragment to a machine.
//
// A Fragment is plain data (it is stored as JSON in the backup zip), so
// restoring never needs the machine it came from.
package components

import (
	"fmt"
	"io/fs"
)

// ArchiveFilesDir is the folder in the backup, and in the fs.FS handed to
// Apply, under which a FileRef's content is kept: files/<root>/<path>.
const ArchiveFilesDir = "files"

// Item is one thing within a sub-component the user can tick or untick,
// such as a single shortcut.
type Item struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Group is a heading to list the item under.
	Group string `json:"group,omitempty"`
}

// KeyValue is one setting in a KConfig file.
type KeyValue struct {
	// Item is the Item this belongs to; empty means it always applies.
	Item string `json:"item,omitempty"`
	// File is the KConfig file, relative to the config folder (~/.config).
	File  string `json:"file"`
	Group string `json:"group"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// FileRef is a file or folder to carry along as it is.
type FileRef struct {
	Item string `json:"item,omitempty"`
	// Root is "config" (~/.config), "data" (~/.local/share) or "home".
	Root string `json:"root"`
	// Path is relative to Root, in slash form.
	Path string `json:"path"`
}

// Fragment is what one sub-component collected.
type Fragment struct {
	// Items is set when the user can pick through the content item by item.
	Items []Item     `json:"items,omitempty"`
	Keys  []KeyValue `json:"keys,omitempty"`
	Files []FileRef  `json:"files,omitempty"`
}

// Selection is the set of Item IDs to act on. A nil Selection means all.
type Selection map[string]bool

// Has reports whether something belonging to item is selected: anything
// with no item always is.
func (s Selection) Has(item string) bool {
	return item == "" || s == nil || s[item]
}

// Report says what an Apply did.
type Report struct {
	Applied []string
	Skipped []string
}

func (r *Report) applied(format string, a ...any) {
	r.Applied = append(r.Applied, sprintf(format, a...))
}
func (r *Report) skipped(format string, a ...any) {
	r.Skipped = append(r.Skipped, sprintf(format, a...))
}

// SubComponent is one selectable part of a Category.
type SubComponent struct {
	ID    string
	Label string
	// Collect gathers the settings from this machine.
	Collect func(env *Env) (*Fragment, error)
	// Apply puts a fragment's selected content on this machine. src holds
	// the fragment's files, under ArchiveFilesDir.
	Apply func(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error)
}

// Category is a top-level entry of the menu.
type Category struct {
	ID    string
	Label string
	Subs  []*SubComponent
}

// Registry returns every category the migrator knows.
func Registry() []*Category {
	return []*Category{themeCategory(), hotkeysCategory()}
}

// Find returns the sub-component with the given IDs, or nil.
func Find(category, sub string) *SubComponent {
	for _, c := range Registry() {
		if c.ID != category {
			continue
		}
		for _, s := range c.Subs {
			if s.ID == sub {
				return s
			}
		}
	}
	return nil
}

// Validate checks a fragment read from a backup before anything acts on it:
// a backup can come from anywhere, and must not be able to name a file
// outside the folders it is meant to restore into.
func (f *Fragment) Validate() error {
	for _, kv := range f.Keys {
		if !fs.ValidPath(kv.File) || kv.File == "." || kv.Group == "" || kv.Key == "" {
			return fmt.Errorf("invalid setting in backup: %+v", kv)
		}
	}
	for _, ref := range f.Files {
		switch ref.Root {
		case "config", "data", "home":
		default:
			return fmt.Errorf("invalid file root %q in backup", ref.Root)
		}
		if !fs.ValidPath(ref.Path) || ref.Path == "." {
			return fmt.Errorf("invalid file path %q in backup", ref.Path)
		}
	}
	return nil
}

// Filter returns the part of the fragment that belongs to the selected
// items (and to no item at all).
func (f *Fragment) Filter(sel Selection) *Fragment {
	out := &Fragment{}
	for _, it := range f.Items {
		if sel.Has(it.ID) {
			out.Items = append(out.Items, it)
		}
	}
	for _, kv := range f.Keys {
		if sel.Has(kv.Item) {
			out.Keys = append(out.Keys, kv)
		}
	}
	for _, ref := range f.Files {
		if sel.Has(ref.Item) {
			out.Files = append(out.Files, ref)
		}
	}
	return out
}

// Empty reports whether there is nothing in the fragment to carry.
func (f *Fragment) Empty() bool { return len(f.Keys) == 0 && len(f.Files) == 0 }
