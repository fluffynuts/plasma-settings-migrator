package flow

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fluffynuts/plasma-settings-migrator/internal/archive"
	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
	"github.com/fluffynuts/plasma-settings-migrator/internal/ui"
)

// BackupOptions are what Backup needs.
type BackupOptions struct {
	Env         *components.Env
	Prompt      ui.Prompter
	Out         io.Writer
	ZipPath     string // asked for when empty
	ToolVersion string
}

// Backup asks what to include, and saves it as a zip. It returns the
// zip's path, or "" if there was nothing to save.
func Backup(o BackupOptions) (string, error) {
	picks, err := choose(o.Prompt, "back up", func(*components.Category, *components.SubComponent) bool { return true })
	if err != nil {
		return "", err
	}
	if len(picks) == 0 {
		say(o.Out, "Nothing selected: no backup made.")
		return "", nil
	}

	var parts []archive.Part
	for _, p := range picks {
		frag, err := p.sub.Collect(o.Env)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", p.sub.Label, err)
		}
		if frag.Empty() {
			say(o.Out, "%s › %s: nothing found on this machine, skipping.", p.cat.Label, p.sub.Label)
			continue
		}
		frag, err = chooseItems(o.Prompt, p.cat.Label+" › "+p.sub.Label+":", frag)
		if err != nil {
			return "", err
		}
		if frag.Empty() {
			say(o.Out, "%s › %s: nothing left selected, skipping.", p.cat.Label, p.sub.Label)
			continue
		}
		parts = append(parts, archive.Part{Included: archive.Included{Category: p.cat.ID, Sub: p.sub.ID}, Fragment: frag})
	}
	if len(parts) == 0 {
		say(o.Out, "Nothing to back up.")
		return "", nil
	}

	zipPath := o.ZipPath
	if zipPath == "" {
		host, _ := os.Hostname()
		def := fmt.Sprintf("plasma-settings-%s-%s.zip", host, time.Now().Format("2006-01-02"))
		if zipPath, err = o.Prompt.Input("Save the backup as:", def); err != nil {
			return "", err
		}
	}
	host, _ := os.Hostname()
	err = archive.Write(zipPath, o.Env, archive.Manifest{
		Tool:          "plasma-settings-migrator",
		ToolVersion:   o.ToolVersion,
		Hostname:      host,
		PlasmaVersion: o.Env.PlasmaVersion,
		PlasmaMajor:   o.Env.PlasmaMajor,
	}, parts)
	if err != nil {
		return "", err
	}
	say(o.Out, "Saved %d component(s) to %s", len(parts), zipPath)
	return zipPath, nil
}
