package commands

import "testing"

func TestValidateCommandInput(t *testing.T) {
	for _, test := range []struct {
		name  string
		input CommandInput
		ok    bool
	}{
		{name: "valid", input: CommandInput{Key: "frontend-tests", Title: "Frontend tests", Argv: []string{"pnpm", "test"}}, ok: true},
		{name: "empty argv", input: CommandInput{Key: "frontend-tests", Title: "Frontend tests"}},
		{name: "empty argument", input: CommandInput{Key: "frontend-tests", Title: "Frontend tests", Argv: []string{"pnpm", " "}}},
		{name: "invalid key", input: CommandInput{Key: "Frontend Tests", Title: "Frontend tests", Argv: []string{"pnpm"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateCommandInput(&test.input)
			if (err == nil) != test.ok {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
