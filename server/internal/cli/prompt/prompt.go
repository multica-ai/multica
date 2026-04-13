// Package prompt provides small wrappers around charmbracelet/huh for
// one-at-a-time interactive selection and text input in the multica CLI.
//
// Every helper refuses to prompt when stdin is not a tty (returns ErrNotTTY),
// so scripts/hooks that call `multica run` without flags fail fast instead of
// hanging on a phantom prompt.
package prompt

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// ErrNotTTY is returned when an interactive prompt is requested but the
// process is not attached to a terminal. Callers should surface a useful
// "missing flag" error instead of hanging.
var ErrNotTTY = errors.New("not a tty; pass the value via a flag")

// ErrCancelled is returned when the user cancels the prompt (Ctrl+C).
var ErrCancelled = errors.New("prompt cancelled")

// Option is one selectable row in a Select prompt.
type Option[T any] struct {
	Label       string
	Description string
	Value       T
}

// IsInteractive reports whether the current stdin is a tty. When false, any
// of the helpers below returns ErrNotTTY.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// Select renders a single-choice picker with built-in fuzzy filtering.
// Arrow keys navigate; typing narrows; Enter selects.
func Select[T any](title string, opts []Option[T]) (T, error) {
	var zero T
	if !IsInteractive() {
		return zero, ErrNotTTY
	}
	if len(opts) == 0 {
		return zero, fmt.Errorf("no options to choose from")
	}
	huhOpts := make([]huh.Option[int], len(opts))
	for i, o := range opts {
		label := o.Label
		if o.Description != "" {
			label = fmt.Sprintf("%s — %s", o.Label, o.Description)
		}
		huhOpts[i] = huh.NewOption(label, i)
	}
	var chosen int
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(title).
				Options(huhOpts...).
				Filtering(true).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return zero, ErrCancelled
		}
		return zero, err
	}
	return opts[chosen].Value, nil
}

// Input reads a single line of text with a prompt. initial is pre-populated.
// Returns the trimmed result.
func Input(title, placeholder, initial string) (string, error) {
	if !IsInteractive() {
		return "", ErrNotTTY
	}
	val := initial
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Placeholder(placeholder).
				Value(&val),
		),
	)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrCancelled
		}
		return "", err
	}
	return strings.TrimSpace(val), nil
}

// Multiline reads multi-line text. Users press Esc+Enter (or Ctrl+D depending
// on terminal) to submit. Returns the raw body (not trimmed so intentional
// leading whitespace survives).
func Multiline(title, placeholder, initial string) (string, error) {
	if !IsInteractive() {
		return "", ErrNotTTY
	}
	val := initial
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title(title).
				Placeholder(placeholder).
				Value(&val),
		),
	)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrCancelled
		}
		return "", err
	}
	return val, nil
}
