// Package flow is the two interactive processes: backup (on the machine
// the settings come from) and restore (on the one they are going to).
package flow

import (
	"fmt"
	"io"
	"sort"

	"github.com/fluffynuts/plasma-settings-migrator/internal/components"
	"github.com/fluffynuts/plasma-settings-migrator/internal/ui"
)

// Session lets the machine be prepared for a restore and told about it
// afterwards: stopping the daemon that owns the shortcuts while they are
// written, and applying changes to the running desktop.
type Session interface {
	// Begin is called before anything is written, with the sub-components
	// ("category/sub") about to be restored, and whether the user wants the
	// changes in the running session or on next login. The returned func is
	// called once everything is written, and returns notes for the user.
	Begin(subs []string, applyNow bool) (end func() []string, err error)
}

type noSession struct{}

func (noSession) Begin([]string, bool) (func() []string, error) {
	return func() []string { return nil }, nil
}

// pick is a sub-component the user chose, in a category they chose.
type pick struct {
	cat *components.Category
	sub *components.SubComponent
}

func (p pick) ref() string { return p.cat.ID + "/" + p.sub.ID }

// choose asks which categories, then which sub-components of each, are
// wanted, from those offered. Nothing is ticked to start with.
func choose(p ui.Prompter, action string, offered func(c *components.Category, s *components.SubComponent) bool) ([]pick, error) {
	var cats []*components.Category
	for _, c := range components.Registry() {
		for _, s := range c.Subs {
			if offered(c, s) {
				cats = append(cats, c)
				break
			}
		}
	}
	if len(cats) == 0 {
		return nil, nil
	}
	opts := make([]ui.Option, len(cats))
	for i, c := range cats {
		opts[i] = ui.Option{Key: c.ID, Label: c.Label}
	}
	keys, err := p.MultiSelect("What do you want to "+action+"?", opts)
	if err != nil {
		return nil, err
	}
	var picks []pick
	for _, c := range cats {
		if !contains(keys, c.ID) {
			continue
		}
		var subs []*components.SubComponent
		var sopts []ui.Option
		for _, s := range c.Subs {
			if offered(c, s) {
				subs = append(subs, s)
				sopts = append(sopts, ui.Option{Key: s.ID, Label: s.Label})
			}
		}
		chosen, err := p.MultiSelect(c.Label+":", sopts)
		if err != nil {
			return nil, err
		}
		for _, s := range subs {
			if contains(chosen, s.ID) {
				picks = append(picks, pick{c, s})
			}
		}
	}
	return picks, nil
}

// chooseItems lets the user untick things within a sub-component, when it
// has items; everything starts ticked. It returns the fragment narrowed to
// what is left.
func chooseItems(p ui.Prompter, title string, f *components.Fragment) (*components.Fragment, error) {
	if len(f.Items) == 0 {
		return f, nil
	}
	items := append([]components.Item(nil), f.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Group < items[j].Group })
	opts := make([]ui.Option, len(items))
	for i, it := range items {
		label := it.Label
		if it.Group != "" {
			label = it.Group + " › " + label
		}
		opts[i] = ui.Option{Key: it.ID, Label: label, Selected: true}
	}
	keys, err := p.MultiSelect(title, opts)
	if err != nil {
		return nil, err
	}
	sel := components.Selection{}
	for _, k := range keys {
		sel[k] = true
	}
	return f.Filter(sel), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func say(w io.Writer, format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
