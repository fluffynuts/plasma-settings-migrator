package bundle

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string, mode os.FileMode) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func parts(t *testing.T, path string) (program, zip string, bundled bool) {
	t.Helper()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	p, _ := io.ReadAll(b.Program)
	if b.Zip == nil {
		return string(p), "", false
	}
	z, _ := io.ReadAll(b.Zip)
	return string(p), string(z), true
}

func TestWriteThenOpen(t *testing.T) {
	dir := t.TempDir()
	exe := write(t, filepath.Join(dir, "tool"), "PROGRAM", 0o755)
	zip := write(t, filepath.Join(dir, "b.zip"), "ZIP", 0o644)

	if p, _, bundled := parts(t, exe); bundled || p != "PROGRAM" {
		t.Errorf("plain program read as %q, bundled %v", p, bundled)
	}
	if Bundled(exe) {
		t.Error("plain program reported as bundled")
	}

	dest := filepath.Join(dir, "out", "bundle")
	if err := Write(dest, exe, zip); err != nil {
		t.Fatal(err)
	}
	if p, z, bundled := parts(t, dest); !bundled || p != "PROGRAM" || z != "ZIP" {
		t.Errorf("bundle read as %q + %q, bundled %v", p, z, bundled)
	}
	if info, err := os.Stat(dest); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("bundle mode: %v %v", info, err)
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error("temp file left behind")
	}
}

func TestABundleOfABundleHoldsOnlyTheNewZip(t *testing.T) {
	dir := t.TempDir()
	exe := write(t, filepath.Join(dir, "tool"), "PROGRAM", 0o755)
	first := filepath.Join(dir, "first")
	if err := Write(first, exe, write(t, filepath.Join(dir, "a.zip"), "OLD", 0o644)); err != nil {
		t.Fatal(err)
	}
	// the program part comes from a bundle, and so does the zip
	second := filepath.Join(dir, "second")
	if err := Write(second, first, write(t, filepath.Join(dir, "b.zip"), "NEW", 0o644)); err != nil {
		t.Fatal(err)
	}
	third := filepath.Join(dir, "third")
	if err := Write(third, second, second); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{second, third} {
		if prog, z, _ := parts(t, p); prog != "PROGRAM" || z != "NEW" {
			t.Errorf("%s read as %q + %q", filepath.Base(p), prog, z)
		}
	}
	// and a bundle can replace itself
	if err := Write(first, first, filepath.Join(dir, "b.zip")); err != nil {
		t.Fatal(err)
	}
	if prog, z, _ := parts(t, first); prog != "PROGRAM" || z != "NEW" {
		t.Errorf("rewritten bundle read as %q + %q", prog, z)
	}
}

func TestWriteRefusesToOverwriteItsZip(t *testing.T) {
	dir := t.TempDir()
	exe := write(t, filepath.Join(dir, "tool"), "PROGRAM", 0o755)
	zip := write(t, filepath.Join(dir, "b.zip"), "ZIP", 0o644)
	if err := Write(zip, exe, zip); err == nil {
		t.Error("wrote the bundle over its own zip")
	}
}

func TestOpenRefusesADamagedTrailer(t *testing.T) {
	p := write(t, filepath.Join(t.TempDir(), "x"), "ab"+magic+"\xff\xff\xff\xff\x00\x00\x00\x00", 0o755)
	if _, err := Open(p); err == nil {
		t.Error("accepted a trailer longer than the file")
	}
}
