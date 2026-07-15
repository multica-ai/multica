package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	piharnesseval "github.com/multica-ai/multica/packages/cerebro-pi-harness-eval"
)

func main() {
	repo := flag.String("repo", "../..", "repository root")
	out := flag.String("out", "", "optional JSON report path")
	flag.Parse()
	deliveries, err := piharnesseval.LoadCases()
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	report := piharnesseval.Run(ctx, *repo, deliveries)
	if *out != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			fatal(err)
		}
	}
	fmt.Print(piharnesseval.Markdown(report))
	if !report.Passed {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
