package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/corpustransfer"
	"github.com/spf13/cobra"
)

const (
	maxCorpusCLIArchiveBytes = int64(2 << 30)
	maxCorpusSidecarBytes    = int64(16 << 20)
	corpusTransferTimeout    = 6 * time.Hour
)

type corpusTransferReceipt struct {
	TransferID  string                         `json:"transfer_id"`
	WorkspaceID string                         `json:"workspace_id"`
	State       string                         `json:"state"`
	Manifest    corpustransfer.Manifest        `json:"manifest"`
	Archive     corpustransfer.ArchiveEnvelope `json:"archive"`
	ExpiresAt   time.Time                      `json:"expires_at"`
	CreatedAt   time.Time                      `json:"created_at"`
	ConfirmedAt *time.Time                     `json:"confirmed_at,omitempty"`
	LastSuccess *time.Time                     `json:"last_success_at,omitempty"`
	Late        bool                           `json:"late"`
	Missing     bool                           `json:"missing"`
	RetryReason *string                        `json:"retry_reason"`
}

type corpusCreateRequest struct {
	IdempotencyKey string                         `json:"idempotency_key"`
	Manifest       corpustransfer.Manifest        `json:"manifest"`
	Archive        corpustransfer.ArchiveEnvelope `json:"archive"`
}

type corpusACKReceipt struct {
	TransferID      string    `json:"transfer_id"`
	SinkID          string    `json:"sink_id"`
	ConfirmedSHA256 string    `json:"confirmed_sha256"`
	AcknowledgedAt  time.Time `json:"acknowledged_at"`
}

var corpusCmd = newCorpusCommand()

func newCorpusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "corpus",
		Short: "Package and transfer corpus archives",
		Long:  "Build and move verified corpus ZIPs through the authenticated workspace transfer channel.",
	}
	cmd.AddCommand(newCorpusPackCommand(), newCorpusSendCommand(), newCorpusStatusCommand(), newCorpusReceiveCommand())
	return cmd
}

func newCorpusPackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack <archive.zip>",
		Short: "Build a canonical manifest and Zip64-capable archive",
		Args:  exactArgs(1),
		RunE:  runCorpusPack,
	}
	cmd.Flags().StringArray("source", nil, "Source root as type=name:path (repeatable)")
	cmd.Flags().String("cutoff", "", "Include files modified at or after this RFC3339 timestamp")
	return cmd
}

func newCorpusSendCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "send <archive.zip>",
		Short:   "Stream an existing corpus ZIP to the current workspace",
		Example: `  multica corpus send "C:\Users\EDY\Desktop\codex-export-皮皮-30d.zip" --source-type codex-export`,
		Args:    exactArgs(1),
		RunE:    runCorpusSend,
	}
	cmd.Flags().String("manifest", "", "Manifest sidecar (defaults to <archive>.manifest.json when present)")
	cmd.Flags().String("source-type", "zip", "Source type for an existing ZIP without a sidecar")
	cmd.Flags().String("source-name", "", "Source name for an existing ZIP (defaults to the archive filename)")
	cmd.Flags().String("idempotency-key", "", "Explicit create idempotency key for controlled retries")
	return cmd
}

func newCorpusStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <transfer-id>",
		Short: "Show durable corpus transfer and delivery state",
		Args:  exactArgs(1),
		RunE:  runCorpusStatus,
	}
}

func newCorpusReceiveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "receive <transfer-id>",
		Short: "Verify, atomically install, and acknowledge a corpus package",
		Args:  exactArgs(1),
		RunE:  runCorpusReceive,
	}
	cmd.Flags().String("output-dir", ".", "Directory in which to install the package")
	cmd.Flags().String("sink-id", "", "Stable sink identity (defaults to a host/output-derived local ID)")
	return cmd
}

func parseCorpusSourceSpec(raw string) (corpustransfer.SourceRoot, error) {
	prefix, rootPath, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(rootPath) == "" {
		return corpustransfer.SourceRoot{}, fmt.Errorf("invalid source %q: want type=name:path", raw)
	}
	sourceType, sourceName, ok := strings.Cut(prefix, "=")
	if !ok || !safeCorpusComponent(sourceType) || !safeCorpusComponent(sourceName) {
		return corpustransfer.SourceRoot{}, fmt.Errorf("invalid source %q: type and name must be safe path components", raw)
	}
	return corpustransfer.SourceRoot{Type: sourceType, Name: sourceName, Path: rootPath}, nil
}

func safeCorpusComponent(value string) bool {
	return value != "" && value != "." && value != ".." && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "/\\\x00")
}

func runCorpusPack(cmd *cobra.Command, args []string) error {
	sourceSpecs, _ := cmd.Flags().GetStringArray("source")
	if len(sourceSpecs) == 0 {
		return fmt.Errorf("at least one --source type=name:path is required")
	}
	roots := make([]corpustransfer.SourceRoot, 0, len(sourceSpecs))
	for _, spec := range sourceSpecs {
		root, err := parseCorpusSourceSpec(spec)
		if err != nil {
			return err
		}
		roots = append(roots, root)
	}
	var cutoff time.Time
	if raw, _ := cmd.Flags().GetString("cutoff"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return fmt.Errorf("parse --cutoff as RFC3339: %w", err)
		}
		cutoff = parsed
	}

	archivePath := filepath.Clean(args[0])
	if err := requireAbsentPath(archivePath); err != nil {
		return err
	}
	manifestPath := archivePath + ".manifest.json"
	if err := requireAbsentPath(manifestPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return fmt.Errorf("create archive parent: %w", err)
	}
	tempParent, err := createCorpusPackWorkspace(roots)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempParent)
	manifest, err := corpustransfer.StageAndBuildManifest(cmd.Context(), roots, cutoff, filepath.Join(tempParent, "staging"))
	if err != nil {
		return err
	}
	envelope, err := corpustransfer.BuildZIP(filepath.Join(tempParent, "staging"), archivePath, manifest)
	if err != nil {
		return err
	}
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	if err := writePrivateFileAtomically(manifestPath, manifestJSON); err != nil {
		_ = os.Remove(archivePath)
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{
		"archive_path": archivePath, "manifest_path": manifestPath,
		"archive_sha256": envelope.SHA256, "manifest_sha256": envelope.ManifestSHA256,
		"size_bytes": envelope.SizeBytes, "entry_count": manifest.EntryCount,
		"missing_sources": manifest.MissingSources,
	})
}

func runCorpusSend(cmd *cobra.Command, args []string) error {
	client, workspaceID, err := corpusCommandClient(cmd)
	if err != nil {
		return err
	}
	archivePath := filepath.Clean(args[0])
	manifestPath, _ := cmd.Flags().GetString("manifest")
	sourceType, _ := cmd.Flags().GetString("source-type")
	sourceName, _ := cmd.Flags().GetString("source-name")
	if sourceName == "" {
		sourceName = filepath.Base(archivePath)
	}
	if !safeCorpusComponent(sourceType) || !safeCorpusComponent(sourceName) {
		return fmt.Errorf("source type and name must be safe path components")
	}
	manifest, envelope, err := inspectCorpusArchive(archivePath, manifestPath, corpustransfer.SourceInfo{
		Adapter: "zip", Version: "v1", Type: sourceType, Name: sourceName,
	})
	if err != nil {
		return err
	}
	if envelope.SizeBytes < 1 || envelope.SizeBytes > maxCorpusCLIArchiveBytes {
		return fmt.Errorf("archive size %d exceeds the 2 GiB P0 limit", envelope.SizeBytes)
	}
	idempotencyKey, _ := cmd.Flags().GetString("idempotency-key")
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	if len(idempotencyKey) > 128 {
		return fmt.Errorf("idempotency key must be at most 128 characters")
	}

	ctx, cancel := corpusLongContext(cmd)
	defer cancel()
	basePath := "/api/workspaces/" + workspaceID + "/corpus-transfers"
	var receipt corpusTransferReceipt
	if err := client.PostJSON(ctx, basePath, corpusCreateRequest{IdempotencyKey: idempotencyKey, Manifest: manifest, Archive: envelope}, &receipt); err != nil {
		return fmt.Errorf("create corpus transfer: %w", err)
	}
	itemPath := basePath + "/" + receipt.TransferID
	switch receipt.State {
	case "created":
		archive, err := os.Open(archivePath)
		if err != nil {
			return fmt.Errorf("open archive for upload: %w", err)
		}
		uploadErr := client.PutStream(ctx, itemPath+"/content", archive, envelope.SizeBytes, &receipt)
		closeErr := archive.Close()
		if uploadErr != nil {
			return fmt.Errorf("upload corpus archive: %w", uploadErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close corpus archive: %w", closeErr)
		}
		fallthrough
	case "uploaded":
		if err := client.PostJSON(ctx, itemPath+"/complete", nil, &receipt); err != nil {
			return fmt.Errorf("complete corpus transfer: %w", err)
		}
	case "confirmed", "acked":
		// A controlled replay may already have reached a terminal delivery state.
	default:
		return fmt.Errorf("corpus transfer %s is in non-resumable state %q; retry with a new idempotency key", receipt.TransferID, receipt.State)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), receipt)
}

func runCorpusStatus(cmd *cobra.Command, args []string) error {
	client, workspaceID, err := corpusCommandClient(cmd)
	if err != nil {
		return err
	}
	transferID, err := canonicalCorpusTransferID(args[0])
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var receipt corpusTransferReceipt
	if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID+"/corpus-transfers/"+transferID, &receipt); err != nil {
		return fmt.Errorf("get corpus transfer status: %w", err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), receipt)
}

func runCorpusReceive(cmd *cobra.Command, args []string) error {
	client, workspaceID, err := corpusCommandClient(cmd)
	if err != nil {
		return err
	}
	transferID, err := canonicalCorpusTransferID(args[0])
	if err != nil {
		return err
	}
	ctx, cancel := corpusLongContext(cmd)
	defer cancel()
	itemPath := "/api/workspaces/" + workspaceID + "/corpus-transfers/" + transferID
	var receipt corpusTransferReceipt
	if err := client.GetJSON(ctx, itemPath, &receipt); err != nil {
		return fmt.Errorf("get corpus transfer: %w", err)
	}
	if receipt.State != "confirmed" && receipt.State != "acked" {
		return fmt.Errorf("corpus transfer %s is not available (state %q)", transferID, receipt.State)
	}
	if receipt.Missing || !receipt.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("corpus transfer %s content has expired or is missing", transferID)
	}
	manifestJSON, err := receipt.Manifest.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("validate server manifest: %w", err)
	}
	if sha256HexBytes(manifestJSON) != receipt.Archive.ManifestSHA256 {
		return fmt.Errorf("server manifest sha256 does not match archive receipt")
	}
	if receipt.Archive.SizeBytes < 1 || receipt.Archive.SizeBytes > maxCorpusCLIArchiveBytes {
		return fmt.Errorf("server archive size %d exceeds the 2 GiB P0 limit", receipt.Archive.SizeBytes)
	}

	outputDir, _ := cmd.Flags().GetString("output-dir")
	outputDir = filepath.Clean(outputDir)
	sinkID, _ := cmd.Flags().GetString("sink-id")
	if sinkID == "" {
		sinkID, err = defaultCorpusSinkID(outputDir)
		if err != nil {
			return fmt.Errorf("resolve sink identity: %w", err)
		}
	}
	if strings.TrimSpace(sinkID) == "" || len(sinkID) > 255 || strings.ContainsRune(sinkID, '\x00') {
		return fmt.Errorf("sink-id is invalid")
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	destination := filepath.Join(outputDir, receipt.Manifest.PackageID)
	if _, err := os.Lstat(destination); err == nil {
		if err := corpustransfer.VerifyExtracted(destination, receipt.Manifest); err != nil {
			return fmt.Errorf("verify existing installed package before ACK retry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect installed package destination: %w", err)
	} else if err := downloadAndInstallCorpus(ctx, client, itemPath, outputDir, destination, receipt); err != nil {
		return err
	}
	var ack corpusACKReceipt
	if err := client.PostJSON(ctx, itemPath+"/acks", map[string]string{
		"sink_id": sinkID, "confirmed_sha256": receipt.Archive.SHA256,
	}, &ack); err != nil {
		return fmt.Errorf("package installed at %s but ACK failed: %w", destination, err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{
		"installed_path": destination,
		"transfer":       receipt,
		"ack":            ack,
	})
}

func downloadAndInstallCorpus(ctx context.Context, client *cli.APIClient, itemPath, outputDir, destination string, receipt corpusTransferReceipt) error {
	temp, err := os.CreateTemp(outputDir, ".multica-corpus-download-*.zip")
	if err != nil {
		return fmt.Errorf("create private download file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure download file: %w", err)
	}
	hash := sha256.New()
	counter := &countingWriter{writer: io.MultiWriter(temp, hash), max: receipt.Archive.SizeBytes}
	downloadErr := client.DownloadStream(ctx, itemPath+"/content", counter)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if downloadErr != nil {
		return fmt.Errorf("download corpus archive: %w", downloadErr)
	}
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("persist downloaded corpus archive")
	}
	if counter.count != receipt.Archive.SizeBytes {
		return fmt.Errorf("downloaded archive size %d does not match receipt %d", counter.count, receipt.Archive.SizeBytes)
	}
	if digest := hex.EncodeToString(hash.Sum(nil)); digest != receipt.Archive.SHA256 {
		return fmt.Errorf("downloaded archive sha256 %s does not match receipt %s", digest, receipt.Archive.SHA256)
	}
	if err := corpustransfer.ExtractVerified(tempPath, destination, receipt.Manifest); err != nil {
		return fmt.Errorf("verify and install corpus package: %w", err)
	}
	return nil
}

func corpusCommandClient(cmd *cobra.Command) (*cli.APIClient, string, error) {
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return nil, "", err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, "", err
	}
	client.HTTPClient.Timeout = cli.AtLeastAPITimeout(corpusTransferTimeout)
	return client, workspaceID, nil
}

func corpusLongContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), cli.AtLeastAPITimeout(corpusTransferTimeout))
}

func inspectCorpusArchive(archivePath, manifestPath string, source corpustransfer.SourceInfo) (corpustransfer.Manifest, corpustransfer.ArchiveEnvelope, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return corpustransfer.Manifest{}, corpustransfer.ArchiveEnvelope{}, fmt.Errorf("inspect archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return corpustransfer.Manifest{}, corpustransfer.ArchiveEnvelope{}, fmt.Errorf("archive is not a regular file")
	}
	if info.Size() < 1 || info.Size() > maxCorpusCLIArchiveBytes {
		return corpustransfer.Manifest{}, corpustransfer.ArchiveEnvelope{}, fmt.Errorf("archive size %d exceeds the 2 GiB P0 limit", info.Size())
	}
	inspected, envelope, err := corpustransfer.InspectZIP(archivePath, source)
	if err != nil {
		return corpustransfer.Manifest{}, corpustransfer.ArchiveEnvelope{}, err
	}
	if manifestPath == "" {
		candidate := archivePath + ".manifest.json"
		if _, err := os.Stat(candidate); err == nil {
			manifestPath = candidate
		} else if !os.IsNotExist(err) {
			return corpustransfer.Manifest{}, corpustransfer.ArchiveEnvelope{}, fmt.Errorf("inspect manifest sidecar: %w", err)
		}
	}
	if manifestPath == "" {
		return inspected, envelope, nil
	}
	manifest, manifestJSON, err := readCorpusManifest(manifestPath)
	if err != nil {
		return corpustransfer.Manifest{}, corpustransfer.ArchiveEnvelope{}, err
	}
	if err := compareCorpusManifestEntries(manifest, inspected); err != nil {
		return corpustransfer.Manifest{}, corpustransfer.ArchiveEnvelope{}, fmt.Errorf("manifest sidecar does not describe archive: %w", err)
	}
	envelope.ManifestSHA256 = sha256HexBytes(manifestJSON)
	return manifest, envelope, nil
}

func readCorpusManifest(filename string) (corpustransfer.Manifest, []byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return corpustransfer.Manifest{}, nil, fmt.Errorf("open manifest sidecar: %w", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, maxCorpusSidecarBytes+1))
	decoder.DisallowUnknownFields()
	var manifest corpustransfer.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return corpustransfer.Manifest{}, nil, fmt.Errorf("decode manifest sidecar: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return corpustransfer.Manifest{}, nil, fmt.Errorf("manifest sidecar contains trailing data or exceeds %d bytes", maxCorpusSidecarBytes)
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return corpustransfer.Manifest{}, nil, fmt.Errorf("validate manifest sidecar: %w", err)
	}
	return manifest, canonical, nil
}

func compareCorpusManifestEntries(expected, actual corpustransfer.Manifest) error {
	if expected.EntryCount != actual.EntryCount || expected.TotalUncompressedBytes != actual.TotalUncompressedBytes || len(expected.Entries) != len(actual.Entries) {
		return fmt.Errorf("entry totals differ")
	}
	byPath := make(map[string]corpustransfer.Entry, len(actual.Entries))
	for _, entry := range actual.Entries {
		byPath[entry.Path] = entry
	}
	for _, entry := range expected.Entries {
		got, ok := byPath[entry.Path]
		if !ok || got.SizeBytes != entry.SizeBytes || got.SHA256 != entry.SHA256 {
			return fmt.Errorf("entry %q differs", entry.Path)
		}
	}
	return nil
}

func canonicalCorpusTransferID(raw string) (string, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid transfer id: %w", err)
	}
	return parsed.String(), nil
}

func defaultCorpusSinkID(outputDir string) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(abs))
	return fmt.Sprintf("local:%s:%s", hostname, hex.EncodeToString(digest[:8])), nil
}

func requireAbsentPath(filename string) error {
	if _, err := os.Lstat(filename); err == nil {
		return fmt.Errorf("destination already exists: %s", filename)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination %s: %w", filename, err)
	}
	return nil
}

func createCorpusPackWorkspace(roots []corpustransfer.SourceRoot) (string, error) {
	workspace, err := os.MkdirTemp("", "multica-corpus-pack-*")
	if err != nil {
		return "", fmt.Errorf("create private pack workspace: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(workspace)
		}
	}()
	if err := os.Chmod(workspace, 0o700); err != nil {
		return "", fmt.Errorf("secure pack workspace: %w", err)
	}
	for _, root := range roots {
		inside, err := pathWithinCorpusRoot(root.Path, workspace)
		if err != nil {
			return "", fmt.Errorf("compare pack workspace with source root %s: %w", root.Path, err)
		}
		if inside {
			return "", fmt.Errorf("private pack workspace would be inside source root %s; use a narrower source root", root.Path)
		}
	}
	keep = true
	return workspace, nil
}

func pathWithinCorpusRoot(root, candidate string) (bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(rootAbs); resolveErr == nil {
		rootAbs = resolved
	} else if !os.IsNotExist(resolveErr) {
		return false, resolveErr
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false, err
	}
	candidateAbs, err = filepath.EvalSymlinks(candidateAbs)
	if err != nil {
		return false, err
	}
	relative, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false, err
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))), nil
}

func writePrivateFileAtomically(filename string, body []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(filename), ".multica-corpus-manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create manifest temp file: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(body); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, filename); err != nil {
		return err
	}
	keep = true
	return nil
}

func sha256HexBytes(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

type countingWriter struct {
	writer io.Writer
	count  int64
	max    int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	remaining := w.max - w.count
	if remaining <= 0 {
		return 0, fmt.Errorf("download exceeds declared size %d", w.max)
	}
	overflow := int64(len(p)) > remaining
	if overflow {
		p = p[:int(remaining)]
	}
	n, err := w.writer.Write(p)
	w.count += int64(n)
	if err == nil && overflow {
		err = fmt.Errorf("download exceeds declared size %d", w.max)
	}
	return n, err
}
