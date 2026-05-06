package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var attachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Work with attachments",
}

var attachmentDownloadCmd = &cobra.Command{
	Use:   "download <attachment-id>",
	Short: "Download an attachment to a local file",
	Long:  "Download an attachment by its ID to a local file.",
	Example: `  # Download an image attachment to the current directory
  $ multica attachment download abc123

  # Download to a specific directory
  $ multica attachment download abc123 -o /tmp/images`,
	Args:  exactArgs(1),
	RunE:  runAttachmentDownload,
}

func init() {
	attachmentCmd.AddCommand(attachmentDownloadCmd)

	attachmentDownloadCmd.Flags().StringP("output-dir", "o", ".", "Directory to save the downloaded file")
}

func runAttachmentDownload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	// The overall budget is generous because kernel-dump attachments can
	// be multi-GB and the chengdu internal network routinely does
	// 60 MB/s — so 30 min covers 100 GB. Signed URLs themselves expire
	// in 30 min; we don't want to outlast the signature.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Fetch attachment metadata (includes signed download_url).
	var att map[string]any
	if err := client.GetJSON(ctx, "/api/attachments/"+args[0], &att); err != nil {
		return fmt.Errorf("get attachment: %w", err)
	}

	downloadURL := strVal(att, "download_url")
	if downloadURL == "" {
		return fmt.Errorf("attachment has no download URL")
	}

	filename := filepath.Base(strVal(att, "filename"))
	if filename == "" || filename == "." {
		filename = args[0]
	}

	// Write to the output directory.
	outputDir, _ := cmd.Flags().GetString("output-dir")
	destPath := filepath.Join(outputDir, filename)

	// Stream directly to disk. DownloadFile (in-memory) was capped at
	// 100 MB and silently truncated anything bigger — catastrophic for
	// kernel dumps which routinely exceed 1 GB.
	n, err := client.DownloadFileToPath(ctx, downloadURL, destPath)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}

	// Print the absolute path so agents can reference the file.
	abs, err := filepath.Abs(destPath)
	if err != nil {
		abs = destPath
	}
	fmt.Fprintf(os.Stderr, "Downloaded %d bytes to %s\n", n, abs)

	// Also print as JSON for --output json compatibility.
	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":       strVal(att, "id"),
		"filename": filename,
		"path":     abs,
		"size":     n,
	})
}
