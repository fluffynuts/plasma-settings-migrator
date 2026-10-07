// Package ui is the interactive side: the questions the flows ask. It is
// an interface so the flows can be tested without a terminal.
package ui

import (
	"errors"

	"github.com/charmbracelet/huh"
)

// Option is one choice in a list.
type Option struct {
	Key      string
	Label    string
	Selected bool // for MultiSelect: ticked to start with
}

// ErrAborted is returned when the user cancels (Ctrl+C or Esc).
var ErrAborted = errors.New("cancelled")

// Prompter asks the user things.
type Prompter interface {
	// MultiSelect returns the keys of the options the user ticked.
	MultiSelect(title string, opts []Option) ([]string, error)
	// Select returns the key of the option the user chose.
	Select(title string, opts []Option) (string, error)
	Confirm(title string) (bool, error)
	Input(title, def string) (string, error)
}

// Huh asks with charmbracelet/huh forms.
type Huh struct{}

func run(f huh.Field) error {
	err := huh.NewForm(huh.NewGroup(f)).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

func (Huh) MultiSelect(title string, opts []Option) ([]string, error) {
	var chosen []string
	hopts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		hopts[i] = huh.NewOption(o.Label, o.Key).Selected(o.Selected)
	}
	f := huh.NewMultiSelect[string]().
		Title(title).
		Options(hopts...).
		Filterable(true).
		Height(min(len(opts)+3, 22)).
		Value(&chosen)
	return chosen, run(f)
}

func (Huh) Select(title string, opts []Option) (string, error) {
	var chosen string
	hopts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		hopts[i] = huh.NewOption(o.Label, o.Key)
	}
	return chosen, run(huh.NewSelect[string]().Title(title).Options(hopts...).Value(&chosen))
}

func (Huh) Confirm(title string) (bool, error) {
	var yes bool
	return yes, run(huh.NewConfirm().Title(title).Value(&yes))
}

func (Huh) Input(title, def string) (string, error) {
	v := def
	return v, run(huh.NewInput().Title(title).Value(&v))
}
