package commands

import (
	"errors"
	"regexp"
	"strings"
)

var commandKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateCommandInput(input *CommandInput) error {
	input.Key = strings.TrimSpace(input.Key)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if !commandKeyPattern.MatchString(input.Key) {
		return errors.New("key must be lowercase kebab-case")
	}
	if input.Title == "" {
		return errors.New("title is required")
	}
	if len(input.Argv) == 0 {
		return errors.New("argv must contain at least one argument")
	}
	for i := range input.Argv {
		input.Argv[i] = strings.TrimSpace(input.Argv[i])
		if input.Argv[i] == "" {
			return errors.New("argv cannot contain empty arguments")
		}
	}
	return nil
}
