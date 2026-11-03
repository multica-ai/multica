// Package mobilediag is a phone's log and the read other devices of the account
// make of it over the device link. A read is one request from a computer answered
// by the phone's native link, so it needs no open conversation, and a stuck
// JavaScript thread does not stop it.
package mobilediag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/multica-ai/multica/server/internal/daemon/p2p"
)

// Protocol is the stream protocol a phone answers log reads on.
const Protocol protocol.ID = "/multica/mobile-logs/1"

const (
	logsPath       = "/logs"
	defaultMinutes = 60
	maxMinutes     = 10080
	// A suspended phone never answers, so its first byte has a shorter bound than
	// moving the whole read over a slow relay.
	answerTimeout = 30 * time.Second
	readTimeout   = 2 * time.Minute
)

// ErrRemoteReadDisabled means the phone's user turned these reads off.
var ErrRemoteReadDisabled = errors.New("remote log reads are turned off on this phone")

// Handler answers reads of this store for userID. Only devices of the account
// reach it: the stream listener applies the device allowlist.
func (s *Store) Handler(userID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != logsPath {
			http.NotFound(w, r)
			return
		}
		if !s.RemoteReadAllowed() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		minutes, err := strconv.Atoi(r.URL.Query().Get("since_minutes"))
		if err != nil || minutes < 1 || minutes > maxMinutes {
			minutes = defaultMinutes
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = s.WriteRead(w, userID, minutes)
	})
}

// Phone is a phone of this account as this device knows it. ID tells phones apart:
// their names repeat.
type Phone struct {
	ID         string
	Name       string
	Connected  bool
	Connection string
	Peer       peer.ID
}

// Phones lists the phones among svc's devices.
func Phones(svc *p2p.Service) []Phone {
	phones := []Phone{}
	for _, status := range svc.Peers() {
		if status.Profile == "mobile" && strings.HasPrefix(status.DaemonID, "ios-") {
			phones = append(phones, Phone{ID: status.DaemonID, Name: status.DeviceName, Connected: status.Connected, Connection: status.Connection, Peer: status.ID})
		}
	}
	return phones
}

// Fetched describes what a read delivered.
type Fetched struct {
	Bytes int64
	// Complete is whether the closing summary arrived. Without it the file still
	// holds every record received.
	Complete bool
	Summary  Summary
}

// tailWriter remembers the last line written, which is the summary of a complete read.
type tailWriter struct {
	last    []byte
	current []byte
}

const maxTrackedLine = 64 << 10

func (t *tailWriter) Write(data []byte) (int, error) {
	for rest := data; len(rest) > 0; {
		end := bytes.IndexByte(rest, '\n')
		if end < 0 {
			t.extend(rest)
			break
		}
		t.extend(rest[:end])
		t.last, t.current = t.current, t.last[:0]
		rest = rest[end+1:]
	}
	return len(data), nil
}

func (t *tailWriter) extend(part []byte) {
	if len(t.current)+len(part) <= maxTrackedLine {
		t.current = append(t.current, part...)
	}
}

// Fetch reads phone's logs of the last sinceMinutes into dst. An answer cut off
// part way is not an error: Fetched says so, and dst holds what arrived. Failing
// before any answer returns an error and leaves dst empty.
func Fetch(ctx context.Context, svc *p2p.Service, phone Phone, sinceMinutes int, dst io.Writer) (Fetched, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	tunnel := svc.NewTunnel(phone.Peer, Protocol, &http.Transport{DisableKeepAlives: true})
	defer tunnel.Close()
	target := url.URL{Scheme: "http", Host: "phone", Path: logsPath, RawQuery: url.Values{"since_minutes": {strconv.Itoa(sinceMinutes)}}.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Fetched{}, err
	}
	answered := time.AfterFunc(answerTimeout, cancel)
	defer answered.Stop()
	res, err := (&http.Client{Transport: tunnel}).Do(req)
	if err != nil {
		return Fetched{}, err
	}
	defer res.Body.Close()
	answered.Stop()
	if res.StatusCode == http.StatusForbidden {
		return Fetched{}, ErrRemoteReadDisabled
	}
	if res.StatusCode != http.StatusOK {
		return Fetched{}, fmt.Errorf("phone answered HTTP %d", res.StatusCode)
	}
	tail := &tailWriter{}
	written, copyErr := io.Copy(io.MultiWriter(dst, tail), io.LimitReader(res.Body, MaxReadBytes+1))
	fetched := Fetched{Bytes: written}
	if written == 0 && copyErr != nil {
		return fetched, copyErr
	}
	if copyErr == nil && written <= MaxReadBytes && json.Unmarshal(tail.last, &fetched.Summary) == nil && fetched.Summary.Kind == summaryKind {
		fetched.Complete = true
	}
	return fetched, nil
}
