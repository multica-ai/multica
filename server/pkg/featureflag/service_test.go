package featureflag

import (
	"context"
	"testing"
)

func TestServiceNilSafe(t *testing.T) {
	t.Parallel()
	var s *Service
	if s.IsEnabled(context.Background(), "anything", true) != true {
		t.Fatalf("nil Service must honor the default")
	}
	if s.IsEnabled(context.Background(), "anything", false) != false {
		t.Fatalf("nil Service must honor the default")
	}
	if got := s.Variant(context.Background(), "anything", "control"); got != "control" {
		t.Fatalf("nil Service must return the variant default, got %q", got)
	}
	d := s.Decision(context.Background(), "anything", false)
	if d.Reason != ReasonDefault || d.Source != "default" {
		t.Fatalf("nil Service must return ReasonDefault, got %+v", d)
	}
}

func TestServiceNilProvider(t *testing.T) {
	t.Parallel()
	s := NewService(nil)
	if got := s.IsEnabled(context.Background(), "missing", true); got != true {
		t.Fatalf("nil provider must honor the default")
	}
	d := s.Decision(context.Background(), "missing", false)
	if d.Reason != ReasonDefault {
		t.Fatalf("expected ReasonDefault, got %s", d.Reason)
	}
}

func TestServiceUsesProvider(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("billing_new_invoice_email", Rule{Default: true})
	s := NewService(sp)

	if !s.IsEnabled(context.Background(), "billing_new_invoice_email", false) {
		t.Fatalf("static provider should override the false default")
	}
	d := s.Decision(context.Background(), "billing_new_invoice_email", false)
	if d.Reason != ReasonStatic || d.Source != "static" {
		t.Fatalf("expected ReasonStatic from static source, got %+v", d)
	}
	if d.Key != "billing_new_invoice_email" {
		t.Fatalf("decision must echo the requested key, got %q", d.Key)
	}
}

func TestServiceMissingKeyReturnsDefault(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("known", Rule{Default: true})
	s := NewService(sp)

	if s.IsEnabled(context.Background(), "unknown", false) {
		t.Fatalf("unknown key must honor the default")
	}
	d := s.Decision(context.Background(), "unknown", true)
	if d.Reason != ReasonDefault || d.Enabled != true || d.Variant != "on" {
		t.Fatalf("missing key did not produce default decision: %+v", d)
	}
}

func TestServiceVariantFlag(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("checkout_algo", Rule{Default: true, Variant: "experiment-v2"})
	s := NewService(sp)

	if got := s.Variant(context.Background(), "checkout_algo", "control"); got != "experiment-v2" {
		t.Fatalf("expected experiment-v2, got %q", got)
	}
	if got := s.Variant(context.Background(), "unknown_algo", "control"); got != "control" {
		t.Fatalf("missing key must fall through to variant default, got %q", got)
	}
}
