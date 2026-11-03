// Package servicepreview serves explicitly shared IP and localhost services over authenticated device streams.
package servicepreview

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Protocol = "/multica/preview-tcp/1"
const Capability = "remote-service-preview-v1"
const Lease = 30 * time.Minute

type Failure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Status    int    `json:"-"`
}

func (e *Failure) Error() string { return e.Message }
func fail(code string, status int) *Failure {
	return &Failure{Code: code, Message: strings.ReplaceAll(code, "_", " "), Retryable: status >= 500 || code == "service_expired", Status: status}
}
func WriteError(w http.ResponseWriter, err error) {
	e, ok := err.(*Failure)
	if !ok {
		e = fail("preview_proxy_unavailable", 502)
	}
	write(w, e.Status, map[string]any{"error": map[string]any{"kind": e.Code, "message": e.Message, "retryable": e.Retryable}})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func decode(r io.Reader, v any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return fail("invalid_request", 400)
	}
	if d.Decode(new(any)) != io.EOF {
		return fail("invalid_request", 400)
	}
	return nil
}
func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type Policy struct {
	Enabled bool  `json:"enabled"`
	Ports   []int `json:"ports"`
}
type Target struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}
type Create struct {
	Version        int    `json:"version"`
	RequestID      string `json:"request_id"`
	ConversationID string `json:"conversation_id"`
	Target         Target `json:"target"`
}
type Result struct {
	Version    int       `json:"version"`
	ID         string    `json:"service_id"`
	Capability string    `json:"capability"`
	ExpiresAt  time.Time `json:"expires_at"`
	Protocol   string    `json:"protocol"`
}
type grant struct {
	result       Result
	peer         string
	scope        int64
	conversation string
	target       Target
	cancel       context.CancelFunc
	ctx          context.Context
	timer        *time.Timer
	streams      map[net.Conn]struct{}
}
type receipt struct {
	id, fingerprint string
	until           time.Time
}

type Manager struct {
	mu         sync.Mutex
	path       string
	policy     Policy
	grants     map[string]*grant
	receipts   map[string]receipt
	open       func() bool
	check      func(context.Context, int64, string) error
	revoked    uint64
	logger     *slog.Logger
	agentPorts map[string]*agentPort
}

func New(path string, open func() bool, check func(context.Context, int64, string) error, logger *slog.Logger) *Manager {
	m := &Manager{path: path, open: open, check: check, logger: logger, grants: map[string]*grant{}, receipts: map[string]receipt{}, agentPorts: map[string]*agentPort{}, policy: Policy{Ports: []int{}}}
	if data, err := os.ReadFile(path); err == nil {
		var p Policy
		if json.Unmarshal(data, &p) == nil && validPolicy(p) {
			m.policy = p
		}
	}
	return m
}
func validPolicy(p Policy) bool {
	if len(p.Ports) > 64 {
		return false
	}
	for _, port := range p.Ports {
		if port < 1 || port > 65535 {
			return false
		}
	}
	return true
}
func (m *Manager) Policy() Policy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Policy{Enabled: m.policy.Enabled, Ports: append([]int{}, m.policy.Ports...)}
}
func (m *Manager) SetPolicy(p Policy) error {
	if !validPolicy(p) {
		return fail("invalid_request", 400)
	}
	sort.Ints(p.Ports)
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".service-preview-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, m.path); err != nil {
		return err
	}
	m.policy = Policy{Enabled: p.Enabled, Ports: append([]int{}, p.Ports...)}
	if !p.Enabled {
		m.revoked++
		m.clearAgentPortsLocked()
	}
	for id, g := range m.grants {
		if !m.allowedGrant(g) {
			m.removeLocked(id)
		}
	}
	return nil
}
func (m *Manager) allowed(port int) bool {
	if !m.policy.Enabled {
		return false
	}
	for _, p := range m.policy.Ports {
		if port == p {
			return true
		}
	}
	return false
}

// Loopback normalizes aliases without DNS. No caller-supplied domain is resolved to choose a target.
func Loopback(host string) (string, bool) {
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "localhost", true
	}
	if host == "0.0.0.0" {
		return host, true
	}
	ip, err := netip.ParseAddr(host)
	if err == nil && ip.Unmap().IsLoopback() {
		return ip.String(), true
	}
	return "", false
}

// serviceHost accepts literal IP targets without rewriting them to loopback.
// Domains use the viewer's network and never enter the device service tunnel.
func serviceHost(host string) (string, bool) {
	normalized := strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if ip, err := netip.ParseAddr(normalized); err == nil && ip.Zone() == "" {
		return ip.String(), true
	}
	return Loopback(host)
}
func (m *Manager) Create(ctx context.Context, peer string, scope int64, cmd Create) (Result, error) {
	if peer == "" || scope < 1 {
		return Result{}, fail("forbidden", 403)
	}
	host, ok := serviceHost(cmd.Target.Host)
	if cmd.Version != 1 || !ok || cmd.RequestID == "" || len(cmd.RequestID) > 160 || cmd.ConversationID == "" || len(cmd.ConversationID) > 160 || cmd.Target.Port < 1 || cmd.Target.Port > 65535 {
		return Result{}, fail("invalid_request", 400)
	}
	if err := m.check(ctx, scope, cmd.ConversationID); err != nil {
		return Result{}, fail("conversation_not_found", 404)
	}
	m.mu.Lock()
	generation := m.revoked
	m.mu.Unlock()
	if !m.open() {
		return Result{}, fail("service_sharing_disabled", 403)
	}
	cmd.Target.Host = host
	fingerprintBytes, _ := json.Marshal(cmd)
	fingerprint := string(fingerprintBytes)
	key := fmt.Sprintf("%s:%d:%s", peer, scope, cmd.RequestID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if generation != m.revoked {
		return Result{}, fail("service_sharing_disabled", 403)
	}
	registered := m.agentAllowed(scope, cmd.ConversationID, cmd.Target)
	if !m.policy.Enabled && !registered {
		return Result{}, fail("service_sharing_disabled", 403)
	}
	if !m.allowed(cmd.Target.Port) && !registered {
		return Result{}, fail("port_not_allowed", 403)
	}
	now := time.Now()
	for key, rec := range m.receipts {
		if now.After(rec.until) {
			delete(m.receipts, key)
		}
	}
	if rec, found := m.receipts[key]; found {
		if rec.fingerprint != fingerprint {
			return Result{}, fail("request_id_conflict", 409)
		}
		if g := m.grants[rec.id]; g != nil && now.Before(g.result.ExpiresAt) {
			return g.result, nil
		}
		return Result{}, fail("service_expired", 410)
	}
	if len(m.grants) >= 256 || len(m.receipts) >= 1024 {
		return Result{}, fail("preview_connection_limited", 429)
	}
	result := Result{Version: 1, ID: random(), Capability: random(), ExpiresAt: now.Add(Lease).UTC(), Protocol: Protocol}
	m.renewAgent(scope, cmd.ConversationID, cmd.Target)
	grantCtx, cancel := context.WithCancel(context.Background())
	g := &grant{result: result, peer: peer, scope: scope, conversation: cmd.ConversationID, target: cmd.Target, ctx: grantCtx, cancel: cancel, streams: map[net.Conn]struct{}{}}
	g.timer = time.AfterFunc(Lease, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if current := m.grants[result.ID]; current != nil && !time.Now().Before(current.result.ExpiresAt) {
			m.removeLocked(result.ID)
		}
	})
	m.grants[result.ID] = g
	m.receipts[key] = receipt{id: result.ID, fingerprint: fingerprint, until: now.Add(2 * Lease)}
	return result, nil
}
func (m *Manager) removeLocked(id string) {
	if g := m.grants[id]; g != nil {
		g.timer.Stop()
		g.cancel()
		for c := range g.streams {
			_ = c.Close()
		}
		delete(m.grants, id)
	}
}
func (m *Manager) Revoke() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked++
	m.clearAgentPortsLocked()
	for id := range m.grants {
		m.removeLocked(id)
	}
}
func (m *Manager) change(peer string, scope int64, id string, renew bool) (Result, error) {
	if !m.open() {
		return Result{}, fail("service_sharing_disabled", 403)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.grants[id]
	if g == nil {
		if !renew {
			return Result{}, nil
		}
		return Result{}, fail("service_expired", 410)
	}
	if g.peer != peer || g.scope != scope {
		return Result{}, fail("forbidden", 403)
	}
	if !renew {
		m.removeLocked(id)
		return Result{}, nil
	}
	if !m.allowedGrant(g) || !time.Now().Before(g.result.ExpiresAt) {
		m.removeLocked(id)
		return Result{}, fail("service_expired", 410)
	}
	g.result.ExpiresAt = time.Now().Add(Lease).UTC()
	m.renewAgent(g.scope, g.conversation, g.target)
	g.timer.Reset(Lease)
	return g.result, nil
}
func (m *Manager) Control(w http.ResponseWriter, r *http.Request, scope int64, peer string) {
	if peer == "" {
		WriteError(w, fail("forbidden", 403))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/local/v2/preview/services")
	if path == "" && r.Method == http.MethodPost {
		var cmd Create
		if err := decode(http.MaxBytesReader(w, r.Body, 4096), &cmd); err != nil {
			WriteError(w, err)
			return
		}
		result, err := m.Create(r.Context(), peer, scope, cmd)
		if err != nil {
			WriteError(w, err)
			return
		}
		write(w, 201, result)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 2 || len(parts[0]) != 64 {
		http.NotFound(w, r)
		return
	}
	renew := len(parts) == 2 && parts[1] == "renew" && r.Method == http.MethodPost
	if !renew && !(len(parts) == 1 && r.Method == http.MethodDelete) {
		http.NotFound(w, r)
		return
	}
	result, err := m.change(peer, scope, parts[0], renew)
	if err != nil {
		WriteError(w, err)
		return
	}
	if !renew {
		write(w, 200, map[string]bool{"closed": true})
		return
	}
	write(w, 200, result)
}

func (m *Manager) ServeStream(peer string, stream net.Conn) {
	defer stream.Close()
	started, outcome, port := time.Now(), "invalid_request", 0
	var uploaded, downloaded int64
	defer func() {
		if m.logger != nil {
			relayed := false
			if connection, ok := stream.(interface{ Relayed() bool }); ok {
				relayed = connection.Relayed()
			}
			m.logger.Info("service preview: stream closed", "peer", peer, "port", port, "relayed", relayed, "outcome", outcome, "elapsed", time.Since(started), "uploaded_bytes", uploaded, "downloaded_bytes", downloaded)
		}
	}()
	_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	var hello Hello
	if readFrame(stream, &hello) != nil {
		_ = writeFrame(stream, Reply{Version: 1, Error: fail("invalid_request", 400)})
		return
	}
	_ = stream.SetReadDeadline(time.Time{})
	if !m.open() {
		outcome = "service_sharing_disabled"
		_ = writeFrame(stream, Reply{Version: 1, Error: fail("service_sharing_disabled", 403)})
		return
	}
	m.mu.Lock()
	g := m.grants[hello.ServiceID]
	var failure *Failure
	if hello.Version != 1 || len(hello.ConnectionID) == 0 || len(hello.ConnectionID) > 160 {
		failure = fail("invalid_request", 400)
	} else if g == nil || !time.Now().Before(g.result.ExpiresAt) {
		failure = fail("service_expired", 410)
	} else if g.peer != peer || subtle.ConstantTimeCompare([]byte(g.result.Capability), []byte(hello.Capability)) != 1 {
		failure = fail("forbidden", 403)
	} else if !m.allowedGrant(g) {
		failure = fail("port_not_allowed", 403)
	} else {
		count := 0
		for _, item := range m.grants {
			if item.peer == peer && item.scope == g.scope {
				count += len(item.streams)
			}
		}
		if count >= 32 {
			failure = fail("preview_connection_limited", 429)
		}
	}
	if failure != nil {
		outcome = failure.Code
		m.mu.Unlock()
		_ = writeFrame(stream, Reply{Version: 1, Error: failure})
		return
	}
	g.streams[stream] = struct{}{}
	port = g.target.Port
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(g.streams, stream); m.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
	hosts := []string{g.target.Host}
	if g.target.Host == "localhost" {
		hosts = []string{"::1", "127.0.0.1"}
	}
	var upstream net.Conn
	var err error
	for _, host := range hosts {
		upstream, err = (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(g.target.Port)))
		if err == nil {
			break
		}
	}
	cancel()
	if err != nil {
		outcome = "target_unreachable"
		_ = writeFrame(stream, Reply{Version: 1, Error: fail("target_unreachable", 502)})
		return
	}
	defer upstream.Close()
	stop := context.AfterFunc(g.ctx, func() { _ = stream.Close(); _ = upstream.Close() })
	defer stop()
	_ = stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if writeFrame(stream, Reply{Version: 1, OK: true}) != nil {
		return
	}
	_ = stream.SetWriteDeadline(time.Time{})
	outcome = "closed"
	downloaded, uploaded = CopyDuplex(stream, upstream)
}

// CopyDuplex retains independent read/write EOF while ensuring errors tear both ends down.
func CopyDuplex(a, b net.Conn) (toA, toB int64) {
	type result struct {
		bytes int64
		toA   bool
	}
	done := make(chan result, 2)
	copyOne := func(to, from net.Conn, toA bool) {
		bytes, err := io.Copy(to, from)
		if err != nil {
			_ = a.Close()
			_ = b.Close()
		} else if half, ok := to.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite()
		}
		done <- result{bytes, toA}
	}
	go copyOne(a, b, true)
	go copyOne(b, a, false)
	for range 2 {
		value := <-done
		if value.toA {
			toA = value.bytes
		} else {
			toB = value.bytes
		}
	}
	return
}
