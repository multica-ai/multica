package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/mapcollector"
)

type config struct {
	contractPath   string
	keyFile        string
	outputPath     string
	attachmentsDir string
	timeout        time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var cfg config
	flag.StringVar(&cfg.contractPath, "contract", "", "path to the reviewed NEW31-MAP-v1 allowlist contract")
	flag.StringVar(&cfg.keyFile, "hmac-key-file", "", "path to a mode 0600 temporary run-key file; destroyed immediately after reading")
	flag.StringVar(&cfg.outputPath, "output", "", "path for the redacted JSON report")
	flag.StringVar(&cfg.attachmentsDir, "attachments-root", "", "root containing fixture or restored attachment objects")
	flag.DurationVar(&cfg.timeout, "timeout", 30*time.Minute, "collector deadline")
	flag.Parse()
	if err := cfg.validate(); err != nil {
		return err
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required; no default connection is permitted")
	}
	contract, canonical, err := mapcollector.LoadContract(cfg.contractPath)
	if err != nil {
		return err
	}
	key, err := mapcollector.ReadAndDestroyKeyFile(cfg.keyFile)
	if err != nil {
		return err
	}
	defer clear(key)

	baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(baseCtx, cfg.timeout)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	poolConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "new31-map-v1-collector"
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("connect to source fixture: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping source fixture: %w", err)
	}
	report, err := mapcollector.Collect(ctx, pool, contract, canonical, key, cfg.attachmentsDir)
	if err != nil {
		return err
	}
	encoded, err := report.Marshal()
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if err := writeExclusive(cfg.outputPath, encoded); err != nil {
		return err
	}
	if !report.Accepted {
		return fmt.Errorf("collection rejected: %d fail-closed findings; inspect redacted report", len(report.Rejections))
	}
	return nil
}

func (c config) validate() error {
	if c.contractPath == "" || c.keyFile == "" || c.outputPath == "" || c.attachmentsDir == "" {
		return errors.New("--contract, --hmac-key-file, --output, and --attachments-root are required")
	}
	if c.timeout <= 0 || c.timeout > 30*time.Minute {
		return errors.New("--timeout must be greater than zero and no more than 30m")
	}
	for _, input := range []string{c.contractPath, c.keyFile} {
		inputAbs, err := filepath.Abs(input)
		if err != nil {
			return err
		}
		outputAbs, err := filepath.Abs(c.outputPath)
		if err != nil {
			return err
		}
		if inputAbs == outputAbs {
			return errors.New("output path must not overwrite an input")
		}
	}
	return nil
}

func writeExclusive(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create output report: %w", err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("write output report: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync output report: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close output report: %w", err)
	}
	ok = true
	return nil
}
