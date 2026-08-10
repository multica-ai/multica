package main

// FIR-4930 — `--on-behalf-of` on `multica issue create` / `multica issue update`.
//
// Without this flag an agent running under an autopilot can only ever attribute
// a new issue to the autopilot's creator, because the platform derives the human
// from the task chain. An autopilot that files work for many different owners
// (Deploy reviews being the case that surfaced this) therefore stamps the wrong
// person on every issue and fills their inbox with releases they don't own.
//
// Name resolution deliberately reuses the same resolver as `--assignee`, so
// "Nikolaj", a UUID and an 8-char short id all behave identically to every other
// person-taking flag in the CLI. Only members resolve — an agent name is not a
// human and must not be accepted here.

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// onBehalfOfFlag is the single flag name used by both create and update.
const onBehalfOfFlag = "on-behalf-of"

// memberOnlyOnBehalfOfKinds restricts resolution to real humans.
var memberOnlyOnBehalfOfKinds = assigneeKinds{member: true}

// registerOnBehalfOfFlag adds --on-behalf-of to a command.
func registerOnBehalfOfFlag(cmd *cobra.Command, usage string) {
	cmd.Flags().String(onBehalfOfFlag, "", usage)
}

// applyOnBehalfOfFlag resolves --on-behalf-of and writes on_behalf_of_user_id
// into the request body. A no-op when the flag was not passed, so existing
// callers send a byte-identical body.
//
// Presence is read with Flags().Changed, not value-emptiness: `--on-behalf-of ""`
// is a deliberate "clear the stamp" on update, and must not be confused with
// "flag omitted".
func applyOnBehalfOfFlag(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, body map[string]any) error {
	if !cmd.Flags().Changed(onBehalfOfFlag) {
		return nil
	}
	raw, _ := cmd.Flags().GetString(onBehalfOfFlag)
	if raw == "" {
		body["on_behalf_of_user_id"] = ""
		return nil
	}
	_, userID, err := resolveAssignee(ctx, client, raw, memberOnlyOnBehalfOfKinds)
	if err != nil {
		return fmt.Errorf("resolve --%s: %w", onBehalfOfFlag, err)
	}
	body["on_behalf_of_user_id"] = userID
	return nil
}
