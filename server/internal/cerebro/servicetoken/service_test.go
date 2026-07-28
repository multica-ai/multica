package servicetoken

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMintRequiresFeatureAndBoundedExpiry(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		expiresAt *time.Time
		wantErr   error
	}{
		{name: "feature disabled", enabled: false, expiresAt: ptrTime(time.Now().Add(24 * time.Hour)), wantErr: ErrDisabled},
		{name: "missing expiry", enabled: true, wantErr: ErrExpiryRequired},
		{name: "past expiry", enabled: true, expiresAt: ptrTime(time.Now().Add(-time.Hour)), wantErr: ErrExpiryInvalid},
		{name: "expiry beyond maximum", enabled: true, expiresAt: ptrTime(time.Now().Add((MaxExpiryDays*24 + 1) * time.Hour)), wantErr: ErrExpiryInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			store.enabled = tt.enabled
			_, _, err := NewTokenService(store).Mint(
				context.Background(),
				"ws-1",
				"reader",
				[]string{ScopeSkillsRead},
				tt.expiresAt,
				"user-1",
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Mint() error = %v, want %v", err, tt.wantErr)
			}
			if len(store.byHash) != 0 {
				t.Fatal("invalid mint persisted a token")
			}
		})
	}
}

func TestMintStoresOnlyReadScopesAndAuditsIssuance(t *testing.T) {
	store := newFakeStore()
	expiry := time.Now().Add(90 * 24 * time.Hour)
	token, raw, err := NewTokenService(store).Mint(
		context.Background(),
		"ws-1",
		"reader",
		[]string{ScopeIssuesRead, ScopeSkillsRead},
		&expiry,
		"user-1",
	)
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	if raw == "" || token.ExpiresAt == nil {
		t.Fatal("Mint() did not return an expiring one-time secret")
	}
	if len(token.Scopes) != 2 || token.Scopes[0] != ScopeIssuesRead || token.Scopes[1] != ScopeSkillsRead {
		t.Fatalf("scopes = %v", token.Scopes)
	}
	if len(store.audits) != 1 || store.audits[0].Event != "issued" {
		t.Fatalf("audits = %#v, want issued", store.audits)
	}
}
