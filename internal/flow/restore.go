package flow

import (
	"fmt"
	"io"

	"github.com/fluffynuts/plasma-settings-migrator/internal/archive"
	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
	"github.com/fluffynuts/plasma-settings-migrator/internal/ui"
)

// RestoreOptions are what Restore needs.
type RestoreOptions struct {
	Env     *components.Env
	Prompt  ui.Prompter
	Out     io.Writer
	ZipPath string
	Session Session // nil: nothing is done to the running session
}

// Restore applies a backup, as far as the user wants.
func Restore(o RestoreOptions) error {
	b, err := archive.Open(o.ZipPath)
	if err != nil {
		return err
	}
	defer b.Close()
	m := b.Manifest
	say(o.Out, "Backup from %s, made %s (Plasma %s)", orUnknown(m.Hostname), m.Created.Local().Format("2006-01-02 15:04"), orUnknown(m.PlasmaVersion))
	if m.PlasmaMajor != 0 && o.Env.PlasmaMajor != 0 && m.PlasmaMajor != o.Env.PlasmaMajor {
		say(o.Out, "Note: this machine runs Plasma %d, the backup is from Plasma %d: settings that don't exist here are written anyway, and may be ignored.", o.Env.PlasmaMajor, m.PlasmaMajor)
	}

	picks, err := choose(o.Prompt, "restore", func(c *components.Category, s *components.SubComponent) bool {
		return b.Has(c.ID, s.ID)
	})
	if err != nil {
		return err
	}
	if len(picks) == 0 {
		say(o.Out, "Nothing selected: nothing restored.")
		return nil
	}

	type job struct {
		pick
		frag *components.Fragment
	}
	var jobs []job
	for _, p := range picks {
		frag, err := b.Fragment(p.cat.ID, p.sub.ID)
		if err != nil {
			return fmt.Errorf("reading %s from the backup: %w", p.sub.Label, err)
		}
		if frag, err = chooseItems(o.Prompt, p.cat.Label+" › "+p.sub.Label+":", frag); err != nil {
			return err
		}
		if !frag.Empty() {
			jobs = append(jobs, job{p, frag})
		}
	}
	if len(jobs) == 0 {
		say(o.Out, "Nothing left selected: nothing restored.")
		return nil
	}

	mode, err := o.Prompt.Select("Apply the changes:", []ui.Option{
		{Key: "now", Label: "Now, to the running session"},
		{Key: "login", Label: "On next login"},
	})
	if err != nil {
		return err
	}
	session := o.Session
	if session == nil {
		session = noSession{}
	}
	refs := make([]string, len(jobs))
	for i, j := range jobs {
		refs[i] = j.ref()
	}
	end, err := session.Begin(refs, mode == "now")
	if err != nil {
		return err
	}

	var firstErr error
	for _, j := range jobs {
		rep, err := j.sub.Apply(o.Env, j.frag, b.Files(), nil)
		say(o.Out, "\n%s › %s", j.cat.Label, j.sub.Label)
		if rep != nil {
			for _, s := range rep.Applied {
				say(o.Out, "  ✓ %s", s)
			}
			for _, s := range rep.Skipped {
				say(o.Out, "  – skipped: %s", s)
			}
		}
		if err != nil {
			say(o.Out, "  ✗ %s", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", j.sub.Label, err)
			}
		}
	}
	for _, note := range end() {
		say(o.Out, "\n%s", note)
	}
	if o.Env.BackupDir != "" {
		say(o.Out, "\nFiles that were replaced are saved under %s", o.Env.BackupDir)
	}
	return firstErr
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
