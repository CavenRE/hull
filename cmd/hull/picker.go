package main

import (
	"errors"

	"github.com/charmbracelet/huh"
)

// pickMany shows an interactive multi-select (the fzf replacement).
func pickMany(title string, options []string) ([]string, error) {
	if len(options) == 0 {
		return nil, errors.New("nothing to select")
	}
	var selected []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title(title).
			Description("space to toggle, enter to confirm").
			Options(huh.NewOptions(options...)...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

// pickOne shows an interactive single-select.
func pickOne(title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("nothing to select")
	}
	var selected string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Options(huh.NewOptions(options...)...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}

// confirm asks a yes/no question, defaulting to no.
func confirm(title string) (bool, error) {
	ok := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Value(&ok),
	))
	if err := form.Run(); err != nil {
		return false, err
	}
	return ok, nil
}
