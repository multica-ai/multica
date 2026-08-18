package agent

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Env contract between the OpenCode stdin tests and the helper process they
// re-execute. The helper stands in for a natively-installed `opencode.exe`
// (the Chocolatey shim in #6538 is a real PE binary, not a .cmd wrapper), so
// the backend's argv reaches CreateProcess unmediated.
const (
	opencodeStdinHelperEnv      = "MULTICA_OPENCODE_STDIN_HELPER"
	opencodeStdinHelperArgvFile = "MULTICA_OPENCODE_STDIN_HELPER_ARGV_FILE"
	opencodeStdinHelperInFile   = "MULTICA_OPENCODE_STDIN_HELPER_STDIN_FILE"
)

const (
	codeartsModelHelperEnv      = "CODEARTS_MODEL_TEST_HELPER"
	codeartsModelHelperArgvFile = "CODEARTS_MODEL_TEST_HELPER_ARGV_FILE"
)

func runFakeCodeArtsModelHelper() {
	args := os.Args[1:]
	if path := os.Getenv(codeartsModelHelperArgvFile); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(args, "\n")), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "helper: write CodeArts model argv: %v\n", err)
			os.Exit(1)
		}
	}
	for _, arg := range args {
		if arg == "--version" {
			fmt.Println("CodeArts Agent 1.2.3")
			return
		}
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "profile-") {
		fmt.Println("test/" + args[0])
		return
	}
	fmt.Println("huaweicloud-maas/deepseek-v3.2")
}

// runFakeOpencodeStdinHelper records the argv and stdin the backend handed this
// process, then emits a successful OpenCode event stream. It is dispatched from
// TestMain before the testing flag parser runs, which is what lets the process
// be invoked with OpenCode's own argv (`run --format json ...`) instead of
// -test.* flags.
func runFakeOpencodeStdinHelper() {
	argv := strings.Join(os.Args[1:], "\n")
	if err := os.WriteFile(os.Getenv(opencodeStdinHelperArgvFile), []byte(argv), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write argv: %v\n", err)
		os.Exit(1)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read stdin: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv(opencodeStdinHelperInFile), stdin, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write stdin: %v\n", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "models" {
		fmt.Println("huaweicloud-maas/deepseek-v3.2")
		return
	}

	fmt.Println(`{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}`)
	fmt.Println(`{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}`)
	fmt.Println(`{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}`)
}
