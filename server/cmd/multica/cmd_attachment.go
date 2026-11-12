package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

  # Download to a directory inside the working directory (keep agent
  # downloads out of /tmp and other machine-shared paths, MUL-4252)
  $ multica attachment download abc123 -o ./attachments`,
	Args: exactArgs(1),
	RunE: runAttachmentDownload,
}

var attachmentUploadCmd = &cobra.Command{
	Use:   "upload <path>",
	Short: "Upload a file to attach to your chat reply",
	Long: `Upload a local file so it is attached to the reply of the current chat task.

Intended for agents running inside a chat task: the file is tagged with the
task and, when the task completes, the server binds it to the assistant reply
it produces — it appears as an attachment card below your reply even if you
paste nothing. The command also returns a markdown snippet you may paste on its
own line to place the item: files use !file[name](url) (a card), images use
![name](url) (inline).

The task id is read from MULTICA_TASK_ID (set by the daemon inside a task);
override it with --task when needed.`,
	Example: `  # Attach an image to the current chat reply
  $ multica attachment upload ./chart.png`,
	Args: exactArgs(1),
	RunE: runAttachmentUpload,
}

func init() {
	attachmentCmd.AddCommand(attachmentDownloadCmd)
	attachmentCmd.AddCommand(attachmentUploadCmd)

	attachmentDownloadCmd.Flags().StringP("output-dir", "o", ".", "Directory to save the downloaded file")
	attachmentUploadCmd.Flags().String("task", "", "Chat task id to attach to (defaults to MULTICA_TASK_ID)")
}

func runAttachmentUpload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	taskID, _ := cmd.Flags().GetString("task")
	if taskID == "" {
		taskID = client.TaskID
	}
	if taskID == "" {
		return fmt.Errorf("no chat task in context: run inside a chat task (MULTICA_TASK_ID set) or pass --task <id>")
	}

	path := args[0]
	if isHTTPURL(path) {
		return fmt.Errorf("upload accepts a local file path, not a URL: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	att, err := client.UploadChatAttachment(ctx, data, path, taskID)
	if err != nil {
		return fmt.Errorf("upload attachment: %w", err)
	}
	cacheUploadedAttachment(cmd, att, data)

	filename := filepath.Base(path)
	markdown := attachmentMarkdown(filename, att.ContentType, att.MarkdownURL)
	fmt.Fprintln(os.Stderr, "Uploaded:", filename)

	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":           att.ID,
		"filename":     filename,
		"markdown_url": att.MarkdownURL,
		"markdown":     markdown,
	})
}

// attachmentMarkdown renders the markdown snippet that makes an uploaded file
// visible in a body: images inline via `![...]( )`, every other file as a
// block-level attachment card via `!file[...]( )`. The label is escaped so a
// name like `report[v2].pdf` stays one label instead of truncating the
// snippet. Shared by `attachment upload` (prints it for the agent to paste)
// and `issue create --attachment` (appends it to the description).
func attachmentMarkdown(filename, contentType, markdownURL string) string {
	label := escapeMarkdownLabel(filename)
	if strings.HasPrefix(contentType, "image/") {
		return fmt.Sprintf("![%s](%s)", label, markdownURL)
	}
	return fmt.Sprintf("!file[%s](%s)", label, markdownURL)
}

// escapeMarkdownLabel escapes the metacharacters a markdown link/image label
// may not contain unescaped ([ ] ( ) and backslash), so a filename like
// `report[v2].pdf` stays a single valid label instead of truncating the
// snippet. Kept in sync with the renderers' unescape set
// (packages/ui/markdown/file-cards.ts).
func escapeMarkdownLabel(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`[`, `\[`,
		`]`, `\]`,
		`(`, `\(`,
		`)`, `\)`,
	).Replace(s)
}

func runAttachmentDownload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	// Fetch attachment metadata (includes signed download_url). This call is
	// also the access check: a local copy is only ever read after the server
	// has authorized this caller for the attachment.
	var att map[string]any
	if err := client.GetJSON(ctx, "/api/attachments/"+args[0], &att); err != nil {
		return fmt.Errorf("get attachment: %w", err)
	}

	filename := filepath.Base(strVal(att, "filename"))
	if filename == "" || filename == "." {
		filename = args[0]
	}

	data, source, err := readAttachmentBytes(ctx, client, attachmentCacheFor(cmd), att)
	if err != nil {
		return err
	}

	// Write to the output directory, creating it if needed so `-o` works
	// against a directory that does not exist yet (the help example's
	// `-o ./attachments` in a clean workdir).
	outputDir, _ := cmd.Flags().GetString("output-dir")
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	destPath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Print the absolute path so agents can reference the file.
	abs, err := filepath.Abs(destPath)
	if err != nil {
		abs = destPath
	}
	fmt.Fprintf(os.Stderr, "Downloaded: %s (%s)\n", abs, source)

	// Also print as JSON for --output json compatibility.
	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":       strVal(att, "id"),
		"filename": filename,
		"path":     abs,
		"size":     strVal(att, "size_bytes"),
		"source":   source,
	})
}

// Where `attachment download` got the bytes from.
const (
	attachmentSourceLocal  = "local"
	attachmentSourceRemote = "remote"
)

// attachmentDownloader is the slice of the API client the download needs.
type attachmentDownloader interface {
	DownloadFile(ctx context.Context, downloadURL string) ([]byte, error)
}

// readAttachmentBytes reads an authorized attachment from the local cache when
// a copy there matches the server's content hash, and downloads it otherwise.
// A download whose bytes match the hash is cached for the next reader.
// Attachments without a recorded hash always download and are never cached.
func readAttachmentBytes(ctx context.Context, client attachmentDownloader, cache *attachmentCache, att map[string]any) ([]byte, string, error) {
	sum := strVal(att, "content_sha256")
	if data, ok := cache.read(sum); ok {
		return data, attachmentSourceLocal, nil
	}

	downloadURL := strVal(att, "download_url")
	if downloadURL == "" {
		return nil, "", fmt.Errorf("attachment has no download URL")
	}
	data, err := client.DownloadFile(ctx, downloadURL)
	if err != nil {
		return nil, "", fmt.Errorf("download file: %w", err)
	}
	if sum != "" && contentSHA256(data) == sum {
		if err := cache.write(sum, data); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not cache attachment %s locally: %v\n", strVal(att, "id"), err)
		}
	}
	return data, attachmentSourceRemote, nil
}
