package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/spf13/cobra"
)

var mediaCmd = newMediaCmd()

func newMediaCmd() *cobra.Command {
	root := &cobra.Command{Use: "media", Short: "Generate media on an authorized runtime"}
	generate := &cobra.Command{Use: "generate", Short: "Generate media and return its attachment receipt", Args: exactArgs(0), RunE: runMediaGenerate}
	f := generate.Flags()
	f.String("output", "table", "Output format: table or json")
	f.String("capability", "image-generate", "image-generate or image-edit")
	f.String("provider", "auto", "auto, codex, or agy")
	f.String("prompt", "", "Generation prompt")
	f.String("prompt-file", "", "Read the prompt from a UTF-8 file")
	f.StringArray("input", nil, "Input file, optionally role=path (repeatable)")
	f.StringArray("input-attachment", nil, "Existing attachment, optionally role=id (repeatable)")
	f.String("model", "", "Reserved; native image adapters require an empty value")
	f.String("size", "", "Requested output size")
	f.String("aspect", "", "Requested aspect ratio")
	f.String("issue", "", "Issue to receive the output (inferred inside an issue task)")
	f.String("out", "", "Stream completed output to this file; omitted means no binary download")
	f.Bool("async", false, "Return the queued job immediately")
	generate.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	generate.MarkFlagsMutuallyExclusive("async", "out")
	root.AddCommand(generate)
	jobs := &cobra.Command{Use: "job", Short: "Inspect, wait for, or cancel a media job"}
	for _, action := range []string{"get", "wait", "cancel"} {
		cmd := &cobra.Command{Use: action + " <id>", Short: action + " a media job", Args: exactArgs(1), RunE: runMediaJob}
		cmd.Flags().String("output", "table", "Output format: table or json")
		if action == "wait" {
			cmd.Flags().String("out", "", "Stream completed output to this file")
		}
		jobs.AddCommand(cmd)
	}
	root.AddCommand(jobs)
	return root
}

func splitMediaInput(value string) (string, string) {
	role, path, ok := strings.Cut(value, "=")
	if ok && (role == "input" || role == "subject" || role == "scene" || role == "style") {
		return role, path
	}
	return "", value
}

func runMediaGenerate(cmd *cobra.Command, _ []string) error {
	if err := validateMediaOutput(cmd); err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	flag := func(name string) string { v, _ := cmd.Flags().GetString(name); return v }
	req := protocol.MediaRequest{Capability: flag("capability"), Provider: flag("provider"), Prompt: flag("prompt"), Model: flag("model"), Size: flag("size"), Aspect: flag("aspect"), IssueID: flag("issue")}
	if filename := flag("prompt-file"); filename != "" {
		f, err := os.Open(filename)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 32001))
		f.Close()
		if readErr != nil {
			return readErr
		}
		req.Prompt = string(data)
	}
	attachments, _ := cmd.Flags().GetStringArray("input-attachment")
	for _, value := range attachments {
		role, id := splitMediaInput(value)
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("invalid input attachment id")
		}
		req.Inputs = append(req.Inputs, protocol.MediaInput{Role: role, AttachmentID: id})
	}
	local, _ := cmd.Flags().GetStringArray("input")
	// Validate before uploading any input bytes.
	validation := req
	validation.Inputs = append([]protocol.MediaInput(nil), req.Inputs...)
	for _, value := range local {
		role, _ := splitMediaInput(value)
		validation.Inputs = append(validation.Inputs, protocol.MediaInput{Role: role})
	}
	if err := validation.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 35*time.Minute)
	defer cancel()
	for _, value := range local {
		role, filename := splitMediaInput(value)
		att, err := client.UploadMediaInput(ctx, filename)
		if err != nil {
			return err
		}
		req.Inputs = append(req.Inputs, protocol.MediaInput{Role: role, AttachmentID: att.ID})
	}
	var job protocol.MediaJob
	if err := client.PostJSON(ctx, "/api/media/jobs", req, &job); err != nil {
		return err
	}
	async, _ := cmd.Flags().GetBool("async")
	if !async {
		job, err = client.WaitMediaJob(ctx, job.ID, time.Second)
		if err != nil {
			return fmt.Errorf("media job %s: %w", job.ID, err)
		}
	}
	return finishMediaCommand(cmd, client, ctx, job, flag("out"))
}

func runMediaJob(cmd *cobra.Command, args []string) error {
	if err := validateMediaOutput(cmd); err != nil {
		return err
	}
	if _, err := uuid.Parse(args[0]); err != nil {
		return fmt.Errorf("invalid media job id")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 35*time.Minute)
	defer cancel()
	var job protocol.MediaJob
	switch cmd.Name() {
	case "wait":
		job, err = client.WaitMediaJob(ctx, args[0], time.Second)
	case "get":
		err = client.GetJSON(ctx, "/api/media/jobs/"+args[0], &job)
	case "cancel":
		err = client.PostJSON(ctx, "/api/media/jobs/"+args[0]+"/cancel", struct{}{}, &job)
	}
	if err != nil {
		return err
	}
	out, _ := cmd.Flags().GetString("out")
	return finishMediaCommand(cmd, client, ctx, job, out)
}

func finishMediaCommand(cmd *cobra.Command, client *cli.APIClient, ctx context.Context, job protocol.MediaJob, out string) error {
	if out != "" && job.State == "completed" {
		if job.AttachmentID == "" {
			return fmt.Errorf("completed media job has no attachment")
		}
		if err := client.DownloadMediaOutput(ctx, job.AttachmentID, out); err != nil {
			return err
		}
	}
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		if err := cli.PrintJSON(cmd.OutOrStdout(), job); err != nil {
			return err
		}
	} else if format == "table" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", job.ID, job.State)
		if job.AttachmentID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Attachment: %s\n%s\n", job.AttachmentID, job.Markdown)
		}
	} else {
		return fmt.Errorf("output must be table or json")
	}
	if job.State == "failed" {
		return fmt.Errorf("media job %s failed: %s", job.ID, job.Error)
	}
	return nil
}

func validateMediaOutput(cmd *cobra.Command) error {
	format, _ := cmd.Flags().GetString("output")
	if format != "table" && format != "json" {
		return fmt.Errorf("output must be table or json")
	}
	return nil
}
