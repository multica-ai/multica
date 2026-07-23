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
	server := &http.Server{Addr: cfg.addr, Handler: handler, ReadHeaderTimeout: internalBrowserHTTPTimeout, ReadTimeout: internalBrowserHTTPTimeout, WriteTimeout: 2 * internalBrowserHTTPTimeout}
	log.Printf("browser verifier runner listening on %s", cfg.addr)
	log.Fatal(server.ListenAndServe())
}

const internalBrowserHTTPTimeout = 65 * time.Second
