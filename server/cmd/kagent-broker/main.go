// kagent-broker is a loopback-only DEV prototype. It must not be deployed.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagentbroker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kagent-broker: startup or listener failed")
		os.Exit(1)
	}
}
func credential(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 16386))
	if err != nil || len(b) == 16386 {
		return "", fmt.Errorf("credential unavailable")
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}
func run() error {
	var p kagentbroker.Policy
	var kubeURL, a2aURL, kubeFile, a2aFile, reviewFile, cert, key, attemptFile, ownershipPath string
	var dev, initialize bool
	flag.BoolVar(&dev, "dev-prototype", false, "acknowledge NOT READY FOR DEPLOYMENT; independent review and TLS/network qualification required")
	flag.StringVar(&p.Namespace, "namespace", "", "immutable namespace")
	flag.StringVar(&p.AgentID, "agent-id", "", "canonical agent UUID")
	flag.StringVar(&p.AgentName, "agent-name", "", "exact agent name annotation")
	flag.StringVar(&p.ExpectedUID, "uid", "", "leave unset for broker-created ownership; cannot initialize with a preexisting UID")
	flag.StringVar(&p.OwnerToken, "owner-token", "", "operator-persisted ownership UUID (not bearer credential)")
	flag.StringVar(&p.Harness, "harness", "", "qualified harness name")
	flag.StringVar(&p.Model, "model", "", "qualified model name")
	flag.StringVar(&p.Audience, "audience", "", "exact TokenReview audience")
	flag.StringVar(&p.ServiceAccountUsername, "service-account", "", "system:serviceaccount:namespace:name")
	flag.StringVar(&p.ServiceAccountUID, "service-account-uid", "", "exact service-account UID")
	flag.StringVar(&kubeURL, "kubernetes-url", "", "Kagent cluster HTTPS origin")
	flag.StringVar(&a2aURL, "a2a-url", "", "alpha7 gateway HTTPS origin")
	flag.StringVar(&kubeFile, "kubernetes-token-file", "", "read-only Kubernetes credential file")
	flag.StringVar(&a2aFile, "a2a-token-file", "", "gateway credential file")
	flag.StringVar(&reviewFile, "review-token-file", "", "Kagent cluster TokenReview credential file")
	flag.StringVar(&cert, "tls-cert", "", "listener TLS certificate")
	flag.StringVar(&key, "tls-key", "", "listener TLS private key")
	flag.StringVar(&attemptFile, "attempt-journal", "", "private durable send-attempt journal; retain across restarts, never rotate to reset uncertainty")
	flag.StringVar(&ownershipPath, "ownership-journal", "", "private ownership directory; retain across restarts")
	flag.BoolVar(&initialize, "initialize-ownership", false, "explicitly initialize an absent ownership directory; never resets existing state")
	flag.Parse()
	if !dev || flag.NArg() != 0 || cert == "" || key == "" || attemptFile == "" || ownershipPath == "" {
		return fmt.Errorf("configuration required")
	}
	kt, err := credential(kubeFile)
	if err != nil {
		return err
	}
	at, err := credential(a2aFile)
	if err != nil {
		return err
	}
	rt, err := credential(reviewFile)
	if err != nil {
		return err
	}
	reviewer, err := kagentbroker.NewTokenReviewer(kagentbroker.Endpoint{URL: kubeURL, Credential: rt})
	if err != nil {
		return err
	}
	attempts, err := kagentbroker.OpenAttemptJournal(attemptFile)
	if err != nil {
		return err
	}
	defer attempts.Close()
	ownership, err := kagentbroker.OpenOwnershipJournal(ownershipPath, p, initialize)
	if err != nil {
		return err
	}
	defer ownership.Close()
	b, err := kagentbroker.New(kagentbroker.Config{Ownership: ownership, Attempts: attempts, Policy: p, Reviewer: reviewer, Kubernetes: kagentbroker.Endpoint{URL: kubeURL, Credential: kt}, A2A: kagentbroker.Endpoint{URL: a2aURL, Credential: at}})
	if err != nil {
		return err
	}
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8443")
	if err != nil {
		return err
	}
	defer listener.Close()
	fmt.Fprintln(os.Stderr, "DEV ONLY: NOT READY FOR DEPLOYMENT. Restricted create/patch; delete disabled. Built-in ask_user remains present; no zero-tool guarantee.")
	server := http.Server{Handler: b, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}}}
	return server.Serve(tls.NewListener(listener, server.TLSConfig))
}
