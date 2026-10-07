package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
)

func TestWriteThenOpen(t *testing.T) {
	home := t.TempDir()
	env := &components.Env{Home: home, ConfigHome: filepath.Join(home, ".config"), DataHome: filepath.Join(home, ".local/share")}
	os.MkdirAll(filepath.Join(env.DataHome, "icons/Theme/sub"), 0o755)
	os.WriteFile(filepath.Join(env.DataHome, "icons/Theme/index.theme"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(env.DataHome, "icons/Theme/sub/x"), []byte("b"), 0o644)
	os.Symlink("/etc/passwd", filepath.Join(env.DataHome, "icons/Theme/link"))

	frag := &components.Fragment{
		Keys:  []components.KeyValue{{File: "kdeglobals", Group: "Icons", Key: "Theme", Value: "Theme"}},
		Files: []components.FileRef{{Root: "data", Path: "icons/Theme"}},
	}
	zipPath := filepath.Join(t.TempDir(), "out", "b.zip")
	err := Write(zipPath, env, Manifest{Tool: "t", PlasmaMajor: 6}, []Part{
		{Included{"theme", "icon-theme"}, frag},
		{Included{"theme", "other"}, &components.Fragment{Files: frag.Files}}, // same files again
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(zipPath + ".part"); err == nil {
		t.Error("temp file left behind")
	}

	b, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Manifest.FormatVersion != FormatVersion || b.Manifest.PlasmaMajor != 6 || !b.Has("theme", "icon-theme") || b.Has("theme", "fonts") {
		t.Errorf("manifest: %+v", b.Manifest)
	}
	got, err := b.Fragment("theme", "icon-theme")
	if err != nil || got.Keys[0].Value != "Theme" {
		t.Fatalf("fragment: %+v %v", got, err)
	}
	if data, err := os.ReadFile(zipPath); err != nil || len(data) == 0 {
		t.Fatal(err)
	}
	if _, err := b.Files().Open("files/data/icons/Theme/sub/x"); err != nil {
		t.Error(err)
	}
	if _, err := b.Files().Open("files/data/icons/Theme/link"); err == nil {
		t.Error("symlink was followed into the backup")
	}
}

func TestOpenRejectsOtherZips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(p, []byte("not a zip"), 0o644)
	if _, err := Open(p); err == nil {
		t.Error("accepted")
	}
}
