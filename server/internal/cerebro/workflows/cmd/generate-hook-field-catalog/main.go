package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
)

func main() {
	output := flag.String("output", "", "generated TypeScript output path")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "-output is required")
		os.Exit(2)
	}
	if err := os.WriteFile(*output, workflows.GenerateHookFieldTypeScript(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
