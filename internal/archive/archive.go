// Package archive reads and writes the backup zip:
//
//	manifest.json                       what the backup is and holds
//	<category>/<sub>/fragment.json      a sub-component's settings
//	files/<root>/<path>                 the files and folders those refer to
package archive

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
)

// FormatVersion changes when the zip's layout does in a way older versions
// can't read.
const FormatVersion = 1

// Included names a sub-component whose fragment is in the backup.
type Included struct {
	Category string `json:"category"`
	Sub      string `json:"sub"`
}

// Manifest describes a backup.
type Manifest struct {
	FormatVersion int        `json:"formatVersion"`
	Tool          string     `json:"tool"`
	ToolVersion   string     `json:"toolVersion"`
	Created       time.Time  `json:"created"`
	Hostname      string     `json:"hostname,omitempty"`
	PlasmaVersion string     `json:"plasmaVersion,omitempty"`
	PlasmaMajor   int        `json:"plasmaMajor,omitempty"`
	Includes      []Included `json:"includes"`
}

// Part is one sub-component's collected settings, to go in a backup.
type Part struct {
	Included
	Fragment *components.Fragment
}

// Write saves the parts, and the files they refer to (read from env), as a
// zip at zipPath. m's FormatVersion, Created and Includes are filled in here.
func Write(zipPath string, env *components.Env, m Manifest, parts []Part) (err error) {
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(zipPath + ".part")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(out.Name())
		}
	}()
	zw := zip.NewWriter(out)

	m.FormatVersion = FormatVersion
	if m.Created.IsZero() {
		m.Created = time.Now().UTC()
	}
	m.Includes = nil
	written := map[string]bool{}
	for _, p := range parts {
		m.Includes = append(m.Includes, p.Included)
		if err := writeJSON(zw, path.Join(p.Category, p.Sub, "fragment.json"), p.Fragment); err != nil {
			return err
		}
		for _, ref := range p.Fragment.Files {
			if err := writeTree(zw, env, ref, written); err != nil {
				return err
			}
		}
	}
	if err := writeJSON(zw, "manifest.json", m); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), zipPath)
}

func writeJSON(zw *zip.Writer, name string, v any) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// writeTree adds the file or folder ref names, skipping what an earlier
// ref already added.
func writeTree(zw *zip.Writer, env *components.Env, ref components.FileRef, written map[string]bool) error {
	base := env.FilePath(ref)
	info, err := os.Stat(base)
	if err != nil {
		return err
	}
	add := func(p string) error {
		rel, err := filepath.Rel(env.RootPath(ref.Root), p)
		if err != nil {
			return err
		}
		name := path.Join(components.ArchiveFilesDir, ref.Root, filepath.ToSlash(rel))
		if written[name] {
			return nil
		}
		written[name] = true
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, in)
		return err
	}
	if !info.IsDir() {
		return add(base)
	}
	return filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() { // folders, and links, which could point anywhere
			return nil
		}
		return add(p)
	})
}

// Backup is an opened backup zip.
type Backup struct {
	Manifest Manifest
	zr       *zip.ReadCloser
}

// Open reads a backup's manifest.
func Open(zipPath string) (*Backup, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	b := &Backup{zr: zr}
	if err := readJSON(zr, "manifest.json", &b.Manifest); err != nil {
		zr.Close()
		return nil, fmt.Errorf("%s doesn't look like a plasma-settings-migrator backup: %w", zipPath, err)
	}
	if b.Manifest.FormatVersion > FormatVersion {
		zr.Close()
		return nil, fmt.Errorf("%s was made by a newer version of this tool (format %d): upgrade it first", zipPath, b.Manifest.FormatVersion)
	}
	return b, nil
}

// Close releases the zip.
func (b *Backup) Close() error { return b.zr.Close() }

// Fragment reads a sub-component's fragment, checked to be safe to apply.
func (b *Backup) Fragment(category, sub string) (*components.Fragment, error) {
	var f components.Fragment
	if err := readJSON(&b.zr.Reader, path.Join(category, sub, "fragment.json"), &f); err != nil {
		return nil, err
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Files is the backup as a file system, for the files a fragment refers to.
func (b *Backup) Files() fs.FS { return &b.zr.Reader }

// Has reports whether the backup holds the sub-component.
func (b *Backup) Has(category, sub string) bool {
	for _, i := range b.Manifest.Includes {
		if i.Category == category && i.Sub == sub {
			return true
		}
	}
	return false
}

func readJSON(fsys fs.FS, name string, v any) error {
	fh, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer fh.Close()
	return json.NewDecoder(fh).Decode(v)
}
