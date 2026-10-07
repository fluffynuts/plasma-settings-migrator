package appcli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Install copies the running executable into ~/.local/bin, creating that
// folder if need be, then warns if the folder isn't on the PATH. An older
// copy is replaced without asking, since installing and upgrading is what
// this is for.
func Install(out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the running program: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving the running program's path: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binDir := filepath.Join(home, ".local", "bin")
	return installInto(out, exe, binDir)
}

func installInto(out io.Writer, exe, binDir string) error {
	dest := filepath.Join(binDir, filepath.Base(exe))
	if src, err := os.Stat(exe); err == nil {
		if dst, err := os.Stat(dest); err == nil && os.SameFile(src, dst) {
			fmt.Fprintf(out, "%s is already installed at %s\n", AppName, dest)
			return warnIfNotOnPath(out, binDir)
		}
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	_, statErr := os.Stat(dest)
	replacing := statErr == nil
	if err := replaceFile(exe, dest); err != nil {
		return fmt.Errorf("copying %s to %s: %w", filepath.Base(exe), dest, err)
	}
	if replacing {
		fmt.Fprintf(out, "replaced %s\n", dest)
	} else {
		fmt.Fprintf(out, "installed %s\n", dest)
	}
	return warnIfNotOnPath(out, binDir)
}

// replaceFile puts a copy of src at dest, safely even while dest is running
// — an upgrade can't expect every other copy of the program to be closed.
// Writing into a running binary fails ("text file busy"), so the copy is
// written beside dest and renamed into place.
func replaceFile(src, dest string) error {
	tmp := dest + ".new"
	if err := copyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dest, 0o755) // past the umask, so 0755 stays 0755
}

// warnIfNotOnPath tells the user (and never fails) when dir, where the
// program was just installed, isn't on the PATH. What the PATH is depends on
// the user's shell profile, so all it can do is say what to add.
func warnIfNotOnPath(out io.Writer, dir string) error {
	if onPath(os.Getenv("PATH"), dir) {
		return nil
	}
	fmt.Fprintf(out, "WARNING: %s is not on your PATH, so your shell won't find %s yet.\n", dir, AppName)
	fmt.Fprintf(out, "Add it in your shell's profile (~/.profile, ~/.bashrc, ~/.zshrc, config.fish, ...), e.g.:\n")
	fmt.Fprintf(out, "  export PATH=\"%s:$PATH\"\n", dir)
	return nil
}

// onPath reports whether dir is one of the entries of a PATH list, compared
// exactly apart from a trailing slash.
func onPath(list, dir string) bool {
	dir = strings.TrimRight(dir, "/")
	for _, entry := range strings.Split(list, ":") {
		if entry != "" && strings.TrimRight(entry, "/") == dir {
			return true
		}
	}
	return false
}
