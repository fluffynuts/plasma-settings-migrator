package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/fluffynuts/plasma-settings-migrator/internal/appcli"
	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
	"github.com/fluffynuts/plasma-settings-migrator/internal/flow"
	"github.com/fluffynuts/plasma-settings-migrator/internal/live"
	"github.com/fluffynuts/plasma-settings-migrator/internal/ui"
)

func init() {
	appcli.Usage = appcli.AppName + " [options] [backup [<zip>] | restore <zip>]"
	appcli.ExtraHelp = `
Commands:
  backup [<zip>]   choose KDE Plasma settings to collect into a zip file
                   (asks where to save it if <zip> isn't given)
  restore <zip>    choose what to apply from a zip made by backup
                   (with no command, asks which of the two to do)`
	appcli.Examples = append([]string{
		appcli.AppName + " backup my-settings.zip",
		appcli.AppName + " restore my-settings.zip",
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
		if len(args) > 2 {
			return usage(args[0])
		}
		o := flow.BackupOptions{Env: env, Prompt: prompt, Out: os.Stdout, ToolVersion: appcli.FullVersion()}
		if len(args) == 2 {
			o.ZipPath = args[1]
		}
		_, err := flow.Backup(o)
		return err
	case "restore":
		path := ""
		if len(args) == 2 {
			path = args[1]
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
			Session: &live.Session{Env: env},
		})
	}
	return fmt.Errorf("unknown command %q (see %s --help)", args[0], appcli.AppName)
}

func usage(cmd string) error {
	return fmt.Errorf("wrong arguments for %s (see %s --help)", cmd, appcli.AppName)
}
