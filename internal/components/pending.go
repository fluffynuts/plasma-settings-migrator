package components

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fluffynuts/plasma-settings-migrator/internal/bundle"
)

// The login hook: restoring shortcuts while the daemon that owns them runs.
//
// Since Plasma 6 the shortcut daemon (kglobalacceld) runs inside KWin, so
// it can't be stopped while the user is logged in. It keeps every shortcut
// in memory, and whenever it saves (after any shortcut changes) it writes
// each component's group out again from memory, so whatever was written to
// kglobalshortcutsrc meanwhile is lost; nor does it ever read the file
// again until the next login.
//
// So the shortcuts are kept in the pending folder, with a copy of this
// program, and a script is put in ~/.config/plasma-workspace/env/. Plasma
// sources those scripts at login before it starts KWin; this one runs the
// copy's apply-pending, which writes the shortcuts, then removes the
// script and the pending folder, so it happens once.

const (
	pendingShortcuts = "shortcuts.json"
	hookScript       = "plasma-settings-migrator.sh"
	programName      = "plasma-settings-migrator"
)

// PendingDir is where shortcuts wait for the next login.
func PendingDir(env *Env) string {
	return filepath.Join(env.DataHome, programName, "pending")
}

// LoginLog is where the login hook writes what it did.
func LoginLog(env *Env) string {
	return filepath.Join(env.DataHome, programName, "login.log")
}

func hookPath(env *Env) string {
	return filepath.Join(env.ConfigHome, "plasma-workspace", "env", hookScript)
}

// deferShortcuts adds the settings in f (narrowed to what is to be
// written) to the pending ones; a setting already pending is replaced.
func deferShortcuts(env *Env, f *Fragment) error {
	pending, err := readPending(env)
	if err != nil {
		return err
	}
	var keys []KeyValue
	for _, kv := range pending.Keys {
		if !hasKV(f.Keys, kv) {
			keys = append(keys, kv)
		}
	}
	pending.Keys = append(keys, f.Keys...)
	for _, it := range f.Items {
		if _, dup := itemLabels(pending)[it.ID]; !dup {
			pending.Items = append(pending.Items, it)
		}
	}
	if err := os.MkdirAll(PendingDir(env), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(PendingDir(env), pendingShortcuts), data, 0o644)
}

func readPending(env *Env) (*Fragment, error) {
	data, err := os.ReadFile(filepath.Join(PendingDir(env), pendingShortcuts))
	if errors.Is(err, os.ErrNotExist) {
		return &Fragment{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f Fragment
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("reading the pending shortcuts: %w", err)
	}
	return &f, f.Validate()
}

// HasPendingShortcuts reports whether shortcuts are waiting for the next
// login.
func HasPendingShortcuts(env *Env) bool {
	_, err := os.Stat(filepath.Join(PendingDir(env), pendingShortcuts))
	return err == nil
}

// InstallLoginHook makes the next login write the pending shortcuts, using
// a copy of the program in the file program (without any backup bundled
// into it).
func InstallLoginHook(env *Env, program string) error {
	if program == "" {
		return errors.New("this program's file wasn't found, to run at login")
	}
	copyPath := filepath.Join(PendingDir(env), programName)
	if err := copyProgram(program, copyPath); err != nil {
		return fmt.Errorf("copying this program for the login hook: %w", err)
	}
	q := shellQuote
	script := "# Written by plasma-settings-migrator, and removed by the first login that\n" +
		"# runs it: it puts restored shortcuts into kglobalshortcutsrc before KWin\n" +
		"# starts, as KWin would write over them while it runs.\n" +
		q(copyPath) + " apply-pending >>" + q(LoginLog(env)) + " 2>&1 </dev/null\n" +
		"rm -f " + q(hookPath(env)) + "\n"
	if err := os.MkdirAll(filepath.Dir(hookPath(env)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(hookPath(env), []byte(script), 0o644)
}

// ApplyPendingShortcuts writes the pending shortcuts (at login, before
// KWin starts), then removes the login hook and the pending folder.
func ApplyPendingShortcuts(env *Env) (*Report, error) {
	defer RemoveLoginHook(env)
	f, err := readPending(env)
	if err != nil {
		return &Report{}, err
	}
	rep := &Report{}
	labels := itemLabels(f)
	written, err := writeShortcuts(env, f.Keys)
	for _, kv := range written {
		rep.applied("%s", labelOr(labels, kv.Item, kv.Key))
	}
	return rep, err
}

// RemoveLoginHook removes the login hook and anything pending for it.
func RemoveLoginHook(env *Env) {
	os.Remove(hookPath(env))
	os.RemoveAll(PendingDir(env))
}

func copyProgram(program, dest string) error {
	in, err := bundle.Open(program)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest+".part", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in.Program); err != nil {
		out.Close()
		os.Remove(out.Name())
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(out.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(out.Name(), dest)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itemLabels(f *Fragment) map[string]string {
	labels := map[string]string{}
	for _, it := range f.Items {
		labels[it.ID] = it.Label
	}
	return labels
}
