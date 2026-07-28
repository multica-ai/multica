// CEREBRO-PATCH(browser-verifier-runner): Keep the private verifier in the Cerebro fork.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
)

type config struct {
	addr  string
	token string
}

func loadConfig() (config, error) {
	token := strings.TrimSpace(os.Getenv("BROWSER_VERIFIER_TOKEN"))
	if token == "" {
		return config{}, fmt.Errorf("BROWSER_VERIFIER_TOKEN is required")
	}
	addr := strings.TrimSpace(os.Getenv("BROWSER_VERIFIER_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	return config{addr: addr, token: token}, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	handler := internalbrowserqa.NewRunnerHTTPHandler(cfg.token, internalbrowserqa.NewRunner(internalbrowserqa.ExecCommander{}))
	server := newBrowserVerifierServer(cfg.addr, handler)
	log.Printf("browser verifier runner listening on %s", cfg.addr)
	log.Fatal(server.ListenAndServe())
}

const (
	internalBrowserRequestReadTimeout = 65 * time.Second
	internalBrowserWriteMargin        = 30 * time.Second
)

// CEREBRO-PATCH(internal-agent-browser-qa): Keep the private runner alive for the full browser flow.
func newBrowserVerifierServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: internalBrowserRequestReadTimeout,
		ReadTimeout:       internalBrowserRequestReadTimeout,
		// The response contains the final screenshot or classified failure.
		// Never close the socket before the runner's own legitimate ceiling.
		WriteTimeout: internalbrowserqa.MaxVerificationDuration + internalBrowserWriteMargin,
	}
}
