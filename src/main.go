package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/fluffynuts/plasma-settings-migrator/internal/appcli"
	"github.com/fluffynuts/plasma-settings-migrator/internal/archive"
	"github.com/fluffynuts/plasma-settings-migrator/internal/bundle"
	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
	"github.com/fluffynuts/plasma-settings-migrator/internal/flow"
	"github.com/fluffynuts/plasma-settings-migrator/internal/live"
	"github.com/fluffynuts/plasma-settings-migrator/internal/ui"
)

func init() {
	appcli.Usage = appcli.AppName + " [options] [backup [--bundle <file>] [<zip>] | restore [<zip>] | bundle <zip> [<file>]]"
	appcli.ExtraHelp = `
Commands:
  backup [<zip>]   choose KDE Plasma settings to collect into a zip file
                   (asks where to save it if <zip> isn't given), then
                   offers to save a bundle too: one file holding this
                   program and the backup, to copy to the other machine
                   and run there
    --bundle <file>  save the bundle as <file> without asking
  restore [<zip>]  choose what to apply from a zip made by backup, or from
                   a bundle; a bundle restores its own backup by default
  bundle <zip> [<file>]
                   make a bundle from a zip made by backup
                   (named after the zip, without .zip, if <file> isn't given)

With no command, a bundle restores its backup; otherwise this asks whether
to back up or restore. --install on a bundle installs only the program.`
	appcli.Examples = append([]string{
		appcli.AppName + " backup my-settings.zip",
		appcli.AppName + " backup --bundle migrate-settings my-settings.zip",
		appcli.AppName + " restore my-settings.zip",
		appcli.AppName + " bundle my-settings.zip",
	}, appcli.Examples...)
}

func main() {
	// --help, --version, --install and --upgrade live in internal/appcli.
	if handled, exitCode := appcli.Handle(os.Args[1:]); handled {
		os.Exit(exitCode)
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "plasma-settings-migrator manages KDE Plasma settings, so it only runs on Linux")
		os.Exit(1)
	}
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, ui.ErrAborted) {
			fmt.Fprintln(os.Stderr, "cancelled")
			os.Exit(130)
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", appcli.AppName, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	prompt := ui.Huh{}
	self, selfErr := bundle.Self()
	bundled := selfErr == nil && bundle.Bundled(self)
	if len(args) == 0 && bundled {
		args = []string{"restore"}
	}
	if len(args) == 0 {
		choice, err := prompt.Select("What do you want to do?", []ui.Option{
			{Key: "backup", Label: "Back up settings from this machine"},
			{Key: "restore", Label: "Restore settings from a backup"},
		})
		if err != nil {
			return err
		}
		args = []string{choice}
	}
	env := components.NewEnv()
	switch args[0] {
	case "backup":
		o := flow.BackupOptions{Env: env, Prompt: prompt, Out: os.Stdout, ToolVersion: appcli.FullVersion(), Program: self}
		var rest []string
		for i := 1; i < len(args); i++ {
			switch a := args[i]; {
			case a == "--bundle":
				if i+1 >= len(args) {
					return fmt.Errorf("--bundle needs a file name (see %s --help)", appcli.AppName)
				}
				i++
				o.BundlePath = args[i]
			case strings.HasPrefix(a, "--bundle="):
				o.BundlePath = strings.TrimPrefix(a, "--bundle=")
			default:
				rest = append(rest, a)
			}
		}
		if len(rest) > 1 {
			return usage(args[0])
		}
		if len(rest) == 1 {
			o.ZipPath = rest[0]
		}
		if o.BundlePath != "" && selfErr != nil {
			return selfErr
		}
		_, err := flow.Backup(o)
		return err
	case "bundle":
		if len(args) < 2 || len(args) > 3 {
			return usage(args[0])
		}
		if selfErr != nil {
			return selfErr
		}
		zipPath, dest := args[1], flow.BundleName(args[1])
		if len(args) == 3 {
			dest = args[2]
		}
		b, err := archive.Open(zipPath) // only bundle backups
		if err != nil {
			return err
		}
		b.Close()
		return flow.MakeBundle(os.Stdout, dest, self, zipPath)
	case "apply-pending": // run at login by the hook components.InstallLoginHook writes
		return applyPending(env)
	case "restore":
		path := ""
		if len(args) == 2 {
			path = args[1]
		} else if len(args) == 1 && bundled {
			path = self
			fmt.Println("Restoring the backup bundled into this program.")
		} else if len(args) == 1 {
			var err error
			if path, err = prompt.Input("Backup zip to restore:", ""); err != nil {
				return err
			}
		} else {
			return usage(args[0])
		}
		return flow.Restore(flow.RestoreOptions{
			Env: env, Prompt: prompt, Out: os.Stdout, ZipPath: path,
			Session: &live.Session{Env: env, Program: self, Wayland: os.Getenv("XDG_SESSION_TYPE") == "wayland"},
		})
	}
	return fmt.Errorf("unknown command %q (see %s --help)", args[0], appcli.AppName)
}

// applyPending writes the shortcuts a restore left for this login. Its
// output goes to the login log (components.LoginLog).
func applyPending(env *components.Env) error {
	fmt.Printf("%s: applying restored shortcuts\n", time.Now().Format(time.RFC3339))
	rep, err := components.ApplyPendingShortcuts(env)
	for _, s := range rep.Applied {
		fmt.Printf("  ✓ %s\n", s)
	}
	return err
}

func usage(cmd string) error {
	return fmt.Errorf("wrong arguments for %s (see %s --help)", cmd, appcli.AppName)
}
