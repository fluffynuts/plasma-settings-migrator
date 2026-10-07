package kconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `# a leading comment
[ColorEffects:Disabled]
Color=56,56,56
ColorAmount=0

[General]
ColorScheme=BreezeDark
font=Noto Sans,10,-1,5,400,0,0,0,0,0,0,0,0,0,0,1
; odd comment
not a pair

[KDE]
widgetStyle=Breeze
Name[de]=Fenster

[Foo][Bar]
a=b=c
`

func TestRoundTripIsExact(t *testing.T) {
	f, err := ParseString(sample)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.String(); got != sample {
		t.Fatalf("round trip changed the file:\n%s", got)
	}
}

func TestGet(t *testing.T) {
	f, _ := ParseString(sample)
	cases := []struct {
		group, key, want string
		ok               bool
	}{
		{"General", "ColorScheme", "BreezeDark", true},
		{"KDE", "Name[de]", "Fenster", true},
		{"Foo][Bar", "a", "b=c", true},
		{"ColorEffects:Disabled", "Color", "56,56,56", true},
		{"General", "missing", "", false},
		{"Nope", "x", "", false},
	}
	for _, c := range cases {
		got, ok := f.Get(c.group, c.key)
		if got != c.want || ok != c.ok {
			t.Errorf("Get(%q,%q) = %q,%v want %q,%v", c.group, c.key, got, ok, c.want, c.ok)
		}
	}
}

func TestSetReplacesInPlace(t *testing.T) {
	f, _ := ParseString(sample)
	f.Set("General", "ColorScheme", "BreezeLight")
	want := strings.Replace(sample, "ColorScheme=BreezeDark", "ColorScheme=BreezeLight", 1)
	if got := f.String(); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetNewKeyGoesBeforeTrailingBlankLine(t *testing.T) {
	f, _ := ParseString(sample)
	f.Set("KDE", "extra", "1")
	if !strings.Contains(f.String(), "Name[de]=Fenster\nextra=1\n\n[Foo][Bar]") {
		t.Fatalf("got:\n%s", f)
	}
}

func TestSetCreatesGroup(t *testing.T) {
	f := New()
	f.Set("Icons", "Theme", "breeze")
	if got := f.String(); got != "[Icons]\nTheme=breeze\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDuplicateGroupsLastWins(t *testing.T) {
	f, _ := ParseString("[A]\nk=1\n[A]\nk=2\n")
	if v, _ := f.Get("A", "k"); v != "2" {
		t.Fatalf("got %q", v)
	}
	f.Set("A", "k", "3")
	if f.String() != "[A]\nk=1\n[A]\nk=3\n" {
		t.Fatalf("got %q", f.String())
	}
}

func TestDelete(t *testing.T) {
	f, _ := ParseString(sample)
	if !f.Group("General").Delete("font") || f.Group("General").Delete("font") {
		t.Fatal("Delete reported wrongly")
	}
	if strings.Contains(f.String(), "font=") {
		t.Fatal("font still present")
	}
	if !f.DeleteGroup("KDE") || f.DeleteGroup("KDE") || f.Group("KDE") != nil {
		t.Fatal("DeleteGroup misbehaved")
	}
}

func TestPairsSkipsCommentsAndJunk(t *testing.T) {
	f, _ := ParseString(sample)
	var keys []string
	for _, e := range f.Group("General").Pairs() {
		keys = append(keys, e.Key)
	}
	if strings.Join(keys, ",") != "ColorScheme,font" {
		t.Fatalf("got %v", keys)
	}
}

func TestCRLFIsNormalised(t *testing.T) {
	f, _ := ParseString("[A]\r\nk=v\r\n")
	if v, _ := f.Get("A", "k"); v != "v" {
		t.Fatalf("got %q", v)
	}
}

func TestWriteFileKeepsModeAndCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "rc")
	f := New()
	f.Set("A", "k", "v")
	if err := f.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0o600)
	f.Set("A", "k", "w")
	if err := f.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	got, _ := ReadFile(path)
	if v, _ := got.Get("A", "k"); v != "w" {
		t.Errorf("got %q", v)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}
