package agentvault

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeMinter struct {
	token string
	err   error
	got   []VaultAccess
}

func (f *fakeMinter) MintAgentToken(_ context.Context, _ string, access []VaultAccess) (string, error) {
	f.got = access
	return f.token, f.err
}

type fakeLister struct {
	access []Access
	err    error
}

func (f *fakeLister) ListForAgent(_ context.Context, _, _ string) ([]Access, error) {
	return f.access, f.err
}

func TestPrepareSpawnEnvNoAccessReturnsNil(t *testing.T) {
	s := NewService(&fakeMinter{token: "av_agt_x"}, &fakeLister{}, "http://relay.internal:8090", "/tmp/ca.pem")
	env, err := s.PrepareSpawnEnv(context.Background(), "ws", "ag", "sara-agent")
	if err != nil || env != nil {
		t.Fatalf("expected (nil,nil) for no access, got (%v,%v)", env, err)
	}
}

func TestPrepareSpawnEnvMintsScopedTokenAndBuildsEnv(t *testing.T) {
	m := &fakeMinter{token: "av_agt_scoped"}
	l := &fakeLister{access: []Access{{Vault: "multica", Role: "read-only"}, {Vault: "cloudflare", Role: "member"}}}
	s := NewService(m, l, "http://relay.internal:8090", "/tmp/ca.pem")
	env, err := s.PrepareSpawnEnv(context.Background(), "ws", "ag", "sara-agent")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(env[envHTTPSProxy], "av_agt_scoped@relay.internal:8090") {
		t.Fatalf("proxy url missing token userinfo: %q", env[envHTTPSProxy])
	}
	if env[envSSLCertFile] != "/tmp/ca.pem" {
		t.Fatalf("CA not set: %v", env)
	}
	// the minted token must be scoped to exactly the granted vaults+roles
	if len(m.got) != 2 || m.got[0].Vault != "multica" || m.got[1].Role != "member" {
		t.Fatalf("scope not passed through: %v", m.got)
	}
}

func TestPrepareSpawnEnvMintErrorFailsClosed(t *testing.T) {
	m := &fakeMinter{err: errors.New("boom")}
	l := &fakeLister{access: []Access{{Vault: "multica", Role: "read-only"}}}
	s := NewService(m, l, "http://relay.internal:8090", "/tmp/ca.pem")
	if _, err := s.PrepareSpawnEnv(context.Background(), "ws", "ag", "sara-agent"); err == nil {
		t.Fatalf("expected error to fail the claim closed")
	}
}
