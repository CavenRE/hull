package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// isInteractive reports whether stdin is a real terminal. The huh prompts read
// from the controlling TTY, so under piped/redirected stdin they hang or die
// with an opaque error , we fail closed with an actionable message instead.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// errNoTTY explains that an interactive prompt was needed but stdin is not a
// terminal, and points at the non-interactive escape hatches.
func errNoTTY(what string) error {
	return fmt.Errorf("%s needs an interactive terminal; re-run in a terminal, pass the value as an argument, or use --yes/-y where applicable", what)
}

// pickMany shows an interactive multi-select (the fzf replacement).
func pickMany(title string, options []string) ([]string, error) {
	if len(options) == 0 {
		return nil, errors.New("nothing to select")
	}
	if !isInteractive() {
		return nil, errNoTTY("selecting " + title)
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
	if !isInteractive() {
		return "", errNoTTY("selecting " + title)
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

// confirm asks a yes/no question, defaulting to no. --yes/-y short-circuits
// to yes so destructive commands are scriptable.
func confirm(title string) (bool, error) {
	if flagYes {
		return true, nil
	}
	if !isInteractive() {
		return false, errNoTTY(fmt.Sprintf("confirming %q", title))
	}
	ok := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Value(&ok),
	))
	if err := form.Run(); err != nil {
		return false, err
	}
	return ok, nil
}
