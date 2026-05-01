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

var attachmentUploadCmd = &cobra.Command{
	Use:   "upload <file-path>",
	Short: "Upload a file and attach it to an issue, comment, or chat message",
	Long:  "Upload a local file to the workspace and attach it to an issue, comment, or chat message.",
	Example: `  # Upload a file to an issue
  $ multica attachment upload report.html --issue abc123

  # Upload a file to a specific comment
  $ multica attachment upload screenshot.png --comment def456

  # Upload a file to a chat message
  $ multica attachment upload findings.md --chat-message ghi789

  # Upload with a custom display name
  $ multica attachment upload /tmp/output.md --issue abc123 --name "findings.md"`,
	Args: exactArgs(1),
	RunE: runAttachmentUpload,
}

func init() {
	attachmentCmd.AddCommand(attachmentDownloadCmd)
	attachmentCmd.AddCommand(attachmentUploadCmd)

	attachmentDownloadCmd.Flags().StringP("output-dir", "o", ".", "Directory to save the downloaded file")

	attachmentUploadCmd.Flags().String("issue", "", "Issue ID to attach the file to")
	attachmentUploadCmd.Flags().String("comment", "", "Comment ID to attach the file to")
	attachmentUploadCmd.Flags().String("chat-message", "", "Chat message ID to attach the file to")
	attachmentUploadCmd.Flags().String("name", "", "Custom display name for the attachment (defaults to filename)")
}

func runAttachmentUpload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	filePath := args[0]
	if !filepath.IsAbs(filePath) {
		if abs, err := filepath.Abs(filePath); err == nil {
			filePath = abs
		}
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("cannot read file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a file", filePath)
	}
	const maxSize = 100 << 20 // 100 MB
	if info.Size() > maxSize {
		return fmt.Errorf("file is %d bytes, max is %d (100 MB)", info.Size(), maxSize)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	filename, _ := cmd.Flags().GetString("name")
	if filename == "" {
		filename = filepath.Base(filePath)
	}

	issueID, _ := cmd.Flags().GetString("issue")
	commentID, _ := cmd.Flags().GetString("comment")
	chatMessageID, _ := cmd.Flags().GetString("chat-message")
	if issueID == "" && commentID == "" && chatMessageID == "" {
		return fmt.Errorf("one of --issue, --comment, or --chat-message is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := client.UploadAttachmentTo(ctx, data, filename, cli.AttachmentTarget{
		IssueID:       issueID,
		CommentID:     commentID,
		ChatMessageID: chatMessageID,
	})
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Uploaded: %s\n", strVal(result, "filename"))

	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":       strVal(result, "id"),
		"filename": strVal(result, "filename"),
		"url":      strVal(result, "url"),
		"size":     result["size_bytes"],
	})
}

func runAttachmentDownload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	// Download the file content.
	data, err := client.DownloadFile(ctx, downloadURL)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}

	// Write to the output directory.
	outputDir, _ := cmd.Flags().GetString("output-dir")
	destPath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Print the absolute path so agents can reference the file.
	abs, err := filepath.Abs(destPath)
	if err != nil {
		abs = destPath
	}
	fmt.Fprintln(os.Stderr, "Downloaded:", abs)

	// Also print as JSON for --output json compatibility.
	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":       strVal(att, "id"),
		"filename": filename,
		"path":     abs,
		"size":     strVal(att, "size_bytes"),
	})
}
