// Package adapters implements concrete deploy.Adapter values for the
// providers Ship Hub Phase 6 supports out of the box: Vercel, Cloudflare
// Pages, Fly.io, Render, GitHub Actions, and a generic-webhook escape
// hatch. Each adapter file calls deploy.Register from init().
//
// To add a new provider, drop a new file with:
//
//   func init() { deploy.Register(&myAdapter{}) }
//
// and implement the deploy.Adapter contract. No registry lookup or
// route plumbing changes are required — the multi-tenant webhook
// receiver dispatches by URL slug, and the registry exposes whatever
// init() registered.
package adapters

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Required by Vercel's webhook signing.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient is the shared client all adapter HTTP calls use. The 15s
// timeout is generous for the slow-but-not-pathological Vercel API and
// tight enough that a hung provider doesn't pin a poller goroutine.
//
// Tests override this via the package-level overrideClient seam below.
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

// overrideClient lets tests swap in an httptest server. Concurrency-safe
// because tests run sequentially within a package by default; if a test
// goes parallel it must restore the prior value via t.Cleanup.
func overrideClient(c *http.Client) func() {
	prev := httpClient
	httpClient = c
	return func() { httpClient = prev }
}

// hmacHex returns the lowercase hex of HMAC(body, secret) using the
// supplied hash constructor. Centralized so each adapter doesn't repeat
// the new-write-sum dance — the only difference between Vercel (SHA-1)
// and Render (SHA-256) is the constructor.
func hmacHex(h func() hash.Hash, body []byte, secret string) string {
	mac := hmac.New(h, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// constantTimeEqualHex compares two hex strings with constant-time
// semantics — both inputs are decoded to bytes first, then
// subtle.ConstantTimeCompare is used. Wrappers in each adapter call this
// after computing the expected HMAC.
func constantTimeEqualHex(a, b string) bool {
	aBytes, err := hex.DecodeString(strings.TrimSpace(a))
	if err != nil {
		return false
	}
	bBytes, err := hex.DecodeString(strings.TrimSpace(b))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(aBytes, bBytes) == 1
}

// hmacSHA256Hex / hmacSHA1Hex are tiny wrappers so each adapter's
// VerifySignature reads as a single line. The sha1 import has a
// nolint:gosec because we don't choose the algorithm — Vercel does, and
// rejecting the algorithm would mean rejecting every Vercel webhook.
func hmacSHA256Hex(body []byte, secret string) string { return hmacHex(sha256.New, body, secret) }
func hmacSHA1Hex(body []byte, secret string) string   { return hmacHex(sha1.New, body, secret) } //nolint:gosec

// readBody pulls the response body from an http.Response. Centralized
// so the per-adapter poll/rollback paths don't repeat the
// io.ReadAll-then-close-then-status-check sequence.
func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}
