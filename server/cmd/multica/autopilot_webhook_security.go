package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func addWebhookSecurityFlags(cmd *cobra.Command, update bool) {
	cmd.Flags().String("provider", "", "Webhook provider: generic or github; changing provider requires a disabled trigger")
	cmd.Flags().String("signing-secret-file", "", "Read the exact signing secret bytes from a file (16-4096 bytes; no trimming)")
	cmd.Flags().Bool("signing-secret-stdin", false, "Read signing secret bytes from stdin; never pass the secret on the command line")
	if update {
		cmd.Flags().Bool("clear-signing-secret", false, "Clear the signing secret; GitHub triggers must be disabled")
	}
}

func webhookSecurityBody(cmd *cobra.Command, body map[string]any, creating bool) error {
	provider, _ := cmd.Flags().GetString("provider")
	file, _ := cmd.Flags().GetString("signing-secret-file")
	stdin, _ := cmd.Flags().GetBool("signing-secret-stdin")
	clear, _ := cmd.Flags().GetBool("clear-signing-secret")
	if cmd.Flags().Changed("provider") {
		if provider != "generic" && provider != "github" {
			return errors.New("--provider must be generic or github")
		}
		body["provider"] = provider
	}
	count := 0
	if cmd.Flags().Changed("signing-secret-file") {
		count++
	}
	if stdin {
		count++
	}
	if clear {
		count++
	}
	if count > 1 {
		return errors.New("--signing-secret-file, --signing-secret-stdin and --clear-signing-secret are mutually exclusive")
	}
	if creating && body["kind"] != "webhook" && (count > 0 || cmd.Flags().Changed("provider")) {
		return errors.New("webhook security flags require --kind webhook")
	}
	if clear {
		body["clear_signing_secret"] = true
	}
	if stdin || cmd.Flags().Changed("signing-secret-file") {
		reader := cmd.InOrStdin()
		if !stdin {
			f, err := os.Open(file)
			if err != nil {
				return errors.New("cannot open signing secret file")
			}
			defer f.Close()
			reader = f
		}
		secret, err := io.ReadAll(io.LimitReader(reader, 4097))
		if err != nil {
			return errors.New("cannot read signing secret")
		}
		if len(secret) < 16 || len(secret) > 4096 || !utf8.Valid(secret) || strings.TrimSpace(string(secret)) == "" {
			return errors.New("signing secret must contain 16 to 4096 bytes and not be whitespace-only")
		}
		body["signing_secret"] = string(secret)
	}
	if creating && provider == "github" && body["signing_secret"] == nil {
		return errors.New("--provider github requires --signing-secret-file or --signing-secret-stdin")
	}
	return nil
}

func redactAutopilotTriggerResult(result map[string]any) {
	redactAutopilotWebhookCredentials(map[string]any{"triggers": []any{result}})
	delete(result, "signing_secret")
}

// A proxy/older server may echo a rejected body. Keep status-based exit codes,
// but discard the response body before it can reach normal or --debug output.
func webhookSecurityWriteError(action string, err error, body map[string]any) error {
	if body["signing_secret"] != nil {
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) {
			safe := *httpErr
			safe.Body = "webhook configuration request rejected (response body redacted)"
			return autopilotWriteRequestError(action, &safe)
		}
	}
	return autopilotWriteRequestError(action, err)
}
