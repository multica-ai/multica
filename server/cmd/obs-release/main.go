package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return fmt.Errorf("missing command")
	}

	switch args[0] {
	case "publish":
		return runPublish(ctx, args[1:], stdout)
	case "version":
		fmt.Fprintf(stdout, "multica-obs-release %s (commit: %s, built: %s)\n", version, commit, date)
		return nil
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  multica-obs-release publish --source artifacts/cli --bucket multica --prefix cli [flags]
  multica-obs-release version

Flags for publish:
  --source       Local CLI artifacts directory (default: artifacts/cli)
  --bucket       OBS bucket name (default: multica)
  --prefix       OBS object prefix to publish into (default: cli)
  --endpoint     OBS endpoint (env: HUAWEICLOUD_OBS_ENDPOINT)
  --dry-run      Print planned operations without writing to OBS
  --concurrency  Parallel copy/upload workers (default: 4)

Credentials:
  HUAWEICLOUD_OBS_AK
  HUAWEICLOUD_OBS_SK
  HUAWEICLOUD_OBS_ENDPOINT
  HUAWEICLOUD_OBS_SECURITY_TOKEN (optional)`)
}

func runPublish(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(stdout)

	opts := PublishOptions{}
	fs.StringVar(&opts.SourceDir, "source", "artifacts/cli", "local CLI artifacts directory")
	fs.StringVar(&opts.Bucket, "bucket", "multica", "OBS bucket name")
	fs.StringVar(&opts.Prefix, "prefix", "cli", "OBS object prefix")
	endpoint := fs.String("endpoint", os.Getenv("HUAWEICLOUD_OBS_ENDPOINT"), "OBS endpoint")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print planned operations without writing to OBS")
	fs.IntVar(&opts.Concurrency, "concurrency", 4, "parallel copy/upload workers")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	ak := os.Getenv("HUAWEICLOUD_OBS_AK")
	sk := os.Getenv("HUAWEICLOUD_OBS_SK")
	hasCredentials := strings.TrimSpace(ak) != "" && strings.TrimSpace(sk) != "" && strings.TrimSpace(*endpoint) != ""
	if !opts.DryRun && strings.TrimSpace(ak) == "" {
		return fmt.Errorf("HUAWEICLOUD_OBS_AK is required")
	}
	if !opts.DryRun && strings.TrimSpace(sk) == "" {
		return fmt.Errorf("HUAWEICLOUD_OBS_SK is required")
	}
	if !opts.DryRun && strings.TrimSpace(*endpoint) == "" {
		return fmt.Errorf("OBS endpoint is required via --endpoint or HUAWEICLOUD_OBS_ENDPOINT")
	}

	if opts.Timestamp.IsZero() {
		opts.Timestamp = time.Now()
	}

	var store objectStore
	var closeStore func()
	if opts.DryRun {
		if hasCredentials {
			client, err := newOBSClient(ak, sk, *endpoint)
			if err != nil {
				return err
			}
			store = dryRunStore{out: stdout, reader: client}
			closeStore = client.Close
		} else {
			store = dryRunStore{out: stdout}
			fmt.Fprintln(stdout, "DRY-RUN offline mode: OBS credentials or endpoint not fully set, skipping remote list.")
		}
	} else {
		client, err := newOBSClient(ak, sk, *endpoint)
		if err != nil {
			return err
		}
		store = client
		closeStore = client.Close
	}
	if closeStore != nil {
		defer closeStore()
	}

	result, err := Publish(ctx, store, opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Published %d file(s) to obs://%s/%s/\n", result.Uploaded, opts.Bucket, normalizePrefix(opts.Prefix))
	if result.BackupPrefix != "" {
		fmt.Fprintf(stdout, "Backed up %d existing object(s) to obs://%s/%s\n", result.BackupCount, opts.Bucket, result.BackupPrefix)
	}
	if opts.DryRun {
		fmt.Fprintln(stdout, "Dry run complete; no OBS objects were modified.")
	}
	return nil
}

func newOBSClient(ak, sk, endpoint string) (*obsStore, error) {
	if token := strings.TrimSpace(os.Getenv("HUAWEICLOUD_OBS_SECURITY_TOKEN")); token != "" {
		client, err := obs.New(ak, sk, endpoint, obs.WithSecurityToken(token))
		if err != nil {
			return nil, fmt.Errorf("create OBS client: %w", err)
		}
		return &obsStore{client: client}, nil
	}
	client, err := obs.New(ak, sk, endpoint)
	if err != nil {
		return nil, fmt.Errorf("create OBS client: %w", err)
	}
	return &obsStore{client: client}, nil
}

type dryRunStore struct {
	out    io.Writer
	reader objectStore
}

func (s dryRunStore) ListObjects(ctx context.Context, bucket, prefix string) ([]RemoteObject, error) {
	fmt.Fprintf(s.out, "DRY-RUN list obs://%s/%s\n", bucket, prefix)
	if s.reader == nil {
		return nil, nil
	}
	return s.reader.ListObjects(ctx, bucket, prefix)
}

func (s dryRunStore) CopyObject(_ context.Context, bucket, sourceKey, destKey string) error {
	fmt.Fprintf(s.out, "DRY-RUN copy obs://%s/%s -> obs://%s/%s\n", bucket, sourceKey, bucket, destKey)
	return nil
}

func (s dryRunStore) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	fmt.Fprintf(s.out, "DRY-RUN delete %s object(s) from obs://%s\n", strconv.Itoa(len(keys)), bucket)
	return nil
}

func (s dryRunStore) PutFile(_ context.Context, bucket, key, path, contentType string) error {
	fmt.Fprintf(s.out, "DRY-RUN upload %s -> obs://%s/%s (%s)\n", path, bucket, key, contentType)
	return nil
}
