// Package bundle makes and reads single-file bundles: a copy of this program
// with a backup zip added to the end, so one file can be copied to another
// machine and run there to restore the backup.
//
//	<program> <zip> <trailer>
//
// Linux runs the program part and ignores what follows it. The trailer is
// the 8 bytes of magic, then the program's length as a little-endian
// uint64, so the zip can be found exactly and the program can be copied
// without it (by --install, and when a bundle makes another bundle).
package bundle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const magic = "PSMBUNDL"

const trailerLen = int64(len(magic) + 8)

// File is a program file that may have a backup bundled into it.
type File struct {
	f       *os.File
	Program *io.SectionReader // the program, without any bundled backup
	Zip     *io.SectionReader // the bundled backup's zip; nil when there is none
}

// Open opens a program file, finding the backup bundled into it, if any.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	size := info.Size()
	b := &File{f: f, Program: io.NewSectionReader(f, 0, size)}
	if size < trailerLen {
		return b, nil
	}
	trailer := make([]byte, trailerLen)
	if _, err := f.ReadAt(trailer, size-trailerLen); err != nil {
		f.Close()
		return nil, err
	}
	if string(trailer[:len(magic)]) != magic {
		return b, nil
	}
	n := binary.LittleEndian.Uint64(trailer[len(magic):])
	if n == 0 || n > uint64(size-trailerLen) {
		f.Close()
		return nil, fmt.Errorf("%s looks like a bundle, but its trailer is damaged", path)
	}
	prog := int64(n)
	b.Program = io.NewSectionReader(f, 0, prog)
	b.Zip = io.NewSectionReader(f, prog, size-trailerLen-prog)
	return b, nil
}

// Close closes the file.
func (b *File) Close() error { return b.f.Close() }

// Bundled reports whether the file at path has a backup bundled into it.
func Bundled(path string) bool {
	b, err := Open(path)
	if err != nil {
		return false
	}
	defer b.Close()
	return b.Zip != nil
}

// Self is the path of the running program, with symbolic links resolved.
func Self() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating the running program: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", fmt.Errorf("resolving the running program's path: %w", err)
	}
	return exe, nil
}

// Write makes dest a bundle of the program in exe (without any backup
// already bundled into it) and the zip at zipPath, which may itself be a
// bundle, whose zip is then used. dest may be exe itself.
func Write(dest, exe, zipPath string) (err error) {
	if same(dest, zipPath) {
		return errors.New("the bundle can't be written over the zip it holds")
	}
	prog, err := Open(exe)
	if err != nil {
		return err
	}
	defer prog.Close()
	zf, err := Open(zipPath)
	if err != nil {
		return err
	}
	defer zf.Close()
	zipData := zf.Zip
	if zipData == nil {
		zipData = zf.Program // a plain zip: all of it
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	// Written beside dest and renamed into place, so a running dest (the
	// program bundling itself) isn't written into.
	out, err := os.OpenFile(dest+".part", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			out.Close()
			os.Remove(out.Name())
		}
	}()
	n, err := io.Copy(out, prog.Program)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, zipData); err != nil {
		return err
	}
	trailer := binary.LittleEndian.AppendUint64([]byte(magic), uint64(n))
	if _, err := out.Write(trailer); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(out.Name(), 0o755); err != nil { // past the umask
		return err
	}
	return os.Rename(out.Name(), dest)
}

func same(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}
