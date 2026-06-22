package credentialpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// The write API validates layer and setting before it ever touches the database,
// so these guard paths are testable against a Store with a nil queries handle:
// a rejected call must fail with the typed error and never dereference q.

func TestSetRejectsUnknownLayer(t *testing.T) {
	s := &Store{} // q is nil: a valid call would panic, an invalid one must not reach it.
	_, err := s.Set(context.Background(), SetParams{
		Layer:   Layer("galaxy"),
		Setting: SettingAllow,
	})
	if !errors.Is(err, ErrUnknownLayer) {
		t.Fatalf("Set with unknown layer: got %v, want ErrUnknownLayer", err)
	}
}

func TestSetRejectsUnknownSetting(t *testing.T) {
	s := &Store{}
	_, err := s.Set(context.Background(), SetParams{
		Layer:   LayerAgent,
		Setting: Setting("ask"), // 'ask' is a tool-policy setting; credentials have none.
	})
	if !errors.Is(err, ErrUnknownSetting) {
		t.Fatalf("Set with unknown setting: got %v, want ErrUnknownSetting", err)
	}
}

func TestClearRejectsUnknownLayer(t *testing.T) {
	s := &Store{}
	err := s.Clear(context.Background(), pgtype.UUID{}, "bigquery", Layer("galaxy"), pgtype.UUID{})
	if !errors.Is(err, ErrUnknownLayer) {
		t.Fatalf("Clear with unknown layer: got %v, want ErrUnknownLayer", err)
	}
}

func TestListForSubjectRejectsUnknownLayer(t *testing.T) {
	s := &Store{}
	_, err := s.ListForSubject(context.Background(), pgtype.UUID{}, Layer("galaxy"), pgtype.UUID{})
	if !errors.Is(err, ErrUnknownLayer) {
		t.Fatalf("ListForSubject with unknown layer: got %v, want ErrUnknownLayer", err)
	}
}

// validSetting must accept exactly the three credential settings and reject the
// tool-policy 'ask' that has no meaning for a yes/no box reachability.
func TestValidSetting(t *testing.T) {
	for _, s := range []Setting{SettingInherit, SettingAllow, SettingDeny} {
		if !validSetting(s) {
			t.Errorf("validSetting(%q) = false, want true", s)
		}
	}
	for _, s := range []Setting{Setting("ask"), Setting(""), Setting("maybe")} {
		if validSetting(s) {
			t.Errorf("validSetting(%q) = true, want false", s)
		}
	}
}
