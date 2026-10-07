package components

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/fluffynuts/plasma-settings-migrator/internal/kconfig"
)

func sprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }

// KeySpec picks settings out of a KConfig file. Group matches exactly, or
// as a prefix when it ends in "*". Keys lists the keys wanted; empty means
// all of them.
type KeySpec struct {
	File  string
	Group string
	Keys  []string
}

func matchGroup(pattern, name string) bool {
	if p, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(name, p)
	}
	return pattern == name
}

// collectKeys reads what the specs describe from the files in the config
// folder. A file that isn't there simply yields nothing.
func collectKeys(env *Env, item string, specs ...KeySpec) ([]KeyValue, error) {
	var out []KeyValue
	files := map[string]*kconfig.File{}
	for _, spec := range specs {
		f, loaded := files[spec.File]
		if !loaded {
			var err error
			f, err = kconfig.ReadFile(filepath.Join(env.ConfigHome, spec.File))
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			files[spec.File] = f
		}
		if f == nil {
			continue
		}
		for _, g := range f.Groups() {
			if !matchGroup(spec.Group, g.Name) {
				continue
			}
			for _, e := range g.Pairs() {
				if len(spec.Keys) > 0 && !contains(spec.Keys, e.Key) {
					continue
				}
				// A repeated key means the last counts, and Group.Get gives that.
				v, _ := g.Get(e.Key)
				kv := KeyValue{Item: item, File: spec.File, Group: g.Name, Key: e.Key, Value: v}
				if !hasKV(out, kv) {
					out = append(out, kv)
				}
			}
		}
	}
	return out, nil
}

func hasKV(list []KeyValue, kv KeyValue) bool {
	for _, o := range list {
		if o.File == kv.File && o.Group == kv.Group && o.Key == kv.Key {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// existingFiles keeps the refs that exist on this machine.
func existingFiles(env *Env, refs ...FileRef) []FileRef {
	var out []FileRef
	for _, r := range refs {
		if _, err := os.Stat(env.FilePath(r)); err == nil {
			out = append(out, r)
		}
	}
	return out
}

// writeKeys sets the keys in their files, backing each file up first, and
// saves each file once. transform may adjust the value to write (given the
// value already there, if any) or return skip=true with a reason.
func writeKeys(env *Env, kvs []KeyValue, transform func(kv KeyValue, existing *string) (value string, skip string)) (written []KeyValue, skipped map[KeyValue]string, err error) {
	skipped = map[KeyValue]string{}
	files := map[string]*kconfig.File{}
	var order []string
	for _, kv := range kvs {
		f, ok := files[kv.File]
		if !ok {
			f, err = kconfig.ReadFile(filepath.Join(env.ConfigHome, kv.File))
			if os.IsNotExist(err) {
				f, err = kconfig.New(), nil
			}
			if err != nil {
				return nil, nil, err
			}
			files[kv.File] = f
			order = append(order, kv.File)
		}
		value := kv.Value
		if transform != nil {
			var existing *string
			if v, ok := f.Get(kv.Group, kv.Key); ok {
				existing = &v
			}
			var why string
			if value, why = transform(kv, existing); why != "" {
				skipped[kv] = why
				continue
			}
		}
		f.Set(kv.Group, kv.Key, value)
		written = append(written, kv)
	}
	for _, name := range order {
		path := filepath.Join(env.ConfigHome, name)
		if err := env.backup(path); err != nil {
			return nil, nil, err
		}
		if err := files[name].WriteFile(path); err != nil {
			return nil, nil, err
		}
	}
	return written, skipped, nil
}

// restoreFile copies a FileRef's content from the backup (a file or a
// whole folder) to this machine, backing up files it replaces.
func restoreFile(env *Env, src fs.FS, ref FileRef) error {
	from := path.Join(ArchiveFilesDir, ref.Root, ref.Path)
	info, err := fs.Stat(src, from)
	if err != nil {
		return fmt.Errorf("the backup is missing %s: %w", from, err)
	}
	if !info.IsDir() {
		return restoreOne(env, src, from, env.FilePath(ref))
	}
	return fs.WalkDir(src, from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, from)
		return restoreOne(env, src, p, env.FilePath(ref)+filepath.FromSlash(rel))
	})
}

func restoreOne(env *Env, src fs.FS, from, dest string) error {
	if err := env.backup(dest); err != nil {
		return err
	}
	in, err := src.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// applyGeneric is the Apply of sub-components that need nothing special:
// write the selected keys, restore the selected files.
func applyGeneric(env *Env, f *Fragment, src fs.FS, sel Selection) (*Report, error) {
	rep := &Report{}
	var kvs []KeyValue
	for _, kv := range f.Keys {
		if sel.Has(kv.Item) {
			kvs = append(kvs, kv)
		}
	}
	written, _, err := writeKeys(env, kvs, nil)
	if err != nil {
		return rep, err
	}
	if len(written) > 0 {
		rep.applied("%d setting(s)", len(written))
	}
	for _, ref := range f.Files {
		if !sel.Has(ref.Item) {
			continue
		}
		if err := restoreFile(env, src, ref); err != nil {
			return rep, err
		}
		rep.applied("%s/%s", ref.Root, ref.Path)
	}
	return rep, nil
}

// splitUnescaped splits s at each sep not preceded by a backslash.
func splitUnescaped(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == sep:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
