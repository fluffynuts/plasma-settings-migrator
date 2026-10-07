// Package kconfig reads, edits and writes the INI-style files KDE's KConfig
// uses (kdeglobals, kglobalshortcutsrc, ...).
//
// It is built for editing files in place: every line it doesn't touch,
// comments and blank lines included, is written back exactly as it was
// read, and groups and keys keep their order. Values are kept raw: KConfig
// escapes (\n, \s, \t, \\, \x3d, ...) are neither decoded nor encoded, so
// whatever a caller reads from one file can be set in another unchanged.
package kconfig

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// File is a parsed KConfig file.
type File struct {
	// preamble holds the lines before the first group header.
	preamble []*Entry
	groups   []*Group
}

// Group is one [Header] section. Name is the text inside the header's
// outermost brackets, so [Colors:View] is "Colors:View", and a nested group
// [Foo][Bar] is "Foo][Bar". Names are matched exactly as written, flag
// suffixes such as [$i] included.
type Group struct {
	Name    string
	header  string // the header line, as read
	entries []*Entry
}

// Entry is one line of a group: a key=value pair, or a comment or blank
// line kept so the file writes back as it was read.
type Entry struct {
	Key   string
	Value string
	raw   string
	pair  bool
}

// Pair reports whether the entry is a key=value line, as opposed to a
// comment or a blank line.
func (e *Entry) Pair() bool { return e.pair }

// New returns an empty file.
func New() *File { return &File{} }

// Parse reads a KConfig file. It never fails on content: a line that is
// neither a group header nor key=value is kept as-is, like a comment.
func Parse(r io.Reader) (*File, error) {
	f := New()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var cur *Group
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if name, ok := parseHeader(line); ok {
			cur = &Group{Name: name, header: line}
			f.groups = append(f.groups, cur)
			continue
		}
		e := parseEntry(line)
		if cur == nil {
			f.preamble = append(f.preamble, e)
		} else {
			cur.entries = append(cur.entries, e)
		}
	}
	return f, sc.Err()
}

// ParseString is Parse for a string.
func ParseString(s string) (*File, error) { return Parse(strings.NewReader(s)) }

// ReadFile parses the file at path.
func ReadFile(path string) (*File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return Parse(fh)
}

func parseHeader(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if len(t) < 2 || t[0] != '[' || t[len(t)-1] != ']' {
		return "", false
	}
	return t[1 : len(t)-1], true
}

func parseEntry(line string) *Entry {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' || t[0] == ';' {
		return &Entry{raw: line}
	}
	i := strings.IndexByte(t, '=')
	if i <= 0 {
		return &Entry{raw: line}
	}
	return &Entry{
		Key:   strings.TrimSpace(t[:i]),
		Value: strings.TrimSpace(t[i+1:]),
		raw:   line,
		pair:  true,
	}
}

// Groups returns the groups in file order. Names can repeat if the file
// repeats a header.
func (f *File) Groups() []*Group { return f.groups }

// Group finds the group called name. KConfig lets a later section override
// an earlier one with the same header, so when there are several it is the
// last. It returns nil if there is none.
func (f *File) Group(name string) *Group {
	for i := len(f.groups) - 1; i >= 0; i-- {
		if f.groups[i].Name == name {
			return f.groups[i]
		}
	}
	return nil
}

// EnsureGroup returns the group called name, adding it at the end of the
// file if there isn't one.
func (f *File) EnsureGroup(name string) *Group {
	if g := f.Group(name); g != nil {
		return g
	}
	g := &Group{Name: name, header: "[" + name + "]"}
	f.groups = append(f.groups, g)
	return g
}

// DeleteGroup removes every group called name, and reports whether there
// was one.
func (f *File) DeleteGroup(name string) bool {
	kept := f.groups[:0]
	found := false
	for _, g := range f.groups {
		if g.Name == name {
			found = true
			continue
		}
		kept = append(kept, g)
	}
	f.groups = kept
	return found
}

// Get is a shortcut for f.Group(group).Get(key).
func (f *File) Get(group, key string) (string, bool) {
	g := f.Group(group)
	if g == nil {
		return "", false
	}
	return g.Get(key)
}

// Set is a shortcut for f.EnsureGroup(group).Set(key, value).
func (f *File) Set(group, key, value string) {
	f.EnsureGroup(group).Set(key, value)
}

// Pairs returns the group's key=value entries in order.
func (g *Group) Pairs() []*Entry {
	var out []*Entry
	for _, e := range g.entries {
		if e.pair {
			out = append(out, e)
		}
	}
	return out
}

// Get returns the value of key. As with groups, a repeated key means the
// last one counts.
func (g *Group) Get(key string) (string, bool) {
	if e := g.find(key); e != nil {
		return e.Value, true
	}
	return "", false
}

// Set gives key the value, in place if the key is there, else after the
// group's last key (before any trailing blank lines, so groups stay
// separated).
func (g *Group) Set(key, value string) {
	if e := g.find(key); e != nil {
		if e.Value != value {
			e.Value = value
			e.raw = key + "=" + value
		}
		return
	}
	e := &Entry{Key: key, Value: value, raw: key + "=" + value, pair: true}
	at := len(g.entries)
	for at > 0 && strings.TrimSpace(g.entries[at-1].raw) == "" {
		at--
	}
	g.entries = append(g.entries, nil)
	copy(g.entries[at+1:], g.entries[at:])
	g.entries[at] = e
}

// Delete removes key, and reports whether it was there.
func (g *Group) Delete(key string) bool {
	kept := g.entries[:0]
	found := false
	for _, e := range g.entries {
		if e.pair && e.Key == key {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	g.entries = kept
	return found
}

func (g *Group) find(key string) *Entry {
	for i := len(g.entries) - 1; i >= 0; i-- {
		if e := g.entries[i]; e.pair && e.Key == key {
			return e
		}
	}
	return nil
}

// String renders the file.
func (f *File) String() string {
	var b strings.Builder
	f.WriteTo(&b)
	return b.String()
}

// WriteTo writes the file out, every line ending in a newline.
func (f *File) WriteTo(w io.Writer) (int64, error) {
	bw := bufio.NewWriter(w)
	var n int64
	put := func(s string) {
		m, _ := bw.WriteString(s + "\n")
		n += int64(m)
	}
	for _, e := range f.preamble {
		put(e.raw)
	}
	for _, g := range f.groups {
		put(g.header)
		for _, e := range g.entries {
			put(e.raw)
		}
	}
	return n, bw.Flush()
}

// WriteFile saves the file to path, creating parent folders as needed. It
// writes a temporary file beside path and renames it into place, so
// something reading the file (plasmashell, kwin) never sees it half
// written. An existing file's permissions are kept.
func (f *File) WriteFile(path string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := f.WriteTo(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
