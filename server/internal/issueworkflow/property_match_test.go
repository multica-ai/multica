package issueworkflow

import (
	"encoding/json"
	"errors"
	"testing"
)

const (
	testPropID   = "11111111-1111-4111-8111-111111111111"
	testOptBackend = "22222222-2222-4222-8222-222222222222"
	testOptFrontend = "33333333-3333-4333-8333-333333333333"
	testAgentFront = "44444444-4444-4444-8444-444444444444"
)

func propertyMatchStep() Step {
	return NormalizeHandoff(Step{
		StatusKey: "implement",
		Handler: Handler{
			Type:       HandlerPropertyMatch,
			PropertyID: testPropID,
			Cases: []PropertyMatchCase{
				{Value: testOptBackend, Handler: Handler{Type: HandlerAgent, ID: testAgentID}},
				{Value: testOptFrontend, Handler: Handler{Type: HandlerAgent, ID: testAgentFront}},
			},
		},
		Instructions: "Implement the approved plan.",
		NextStatusKey: "code_review",
	})
}

func TestPropertyMatchHandsOffAndRoundTrip(t *testing.T) {
	s := propertyMatchStep()
	if !s.HandsOff() || !UsesPropertyRouting([]Step{s}) {
		t.Fatalf("property_match must hand off: %+v", s)
	}
	if err := ValidateHandoff(s, Definition{Steps: Arrange([]Step{s, {StatusKey: "code_review", Handler: Handler{Type: HandlerNone}}})}); err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeSteps([]Step{s})
	if err != nil {
		t.Fatal(err)
	}
	var raw []map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	handler, ok := raw[0]["handler"].(map[string]any)
	if !ok || handler["type"] != HandlerPropertyMatch {
		t.Fatalf("encode dropped property_match: %s", encoded)
	}
	decoded, err := DecodeSteps(encoded)
	if err != nil || len(decoded) != 1 || !stepsEqualLocal(decoded[0], s) {
		t.Fatalf("roundtrip=%+v err=%v", decoded, err)
	}
}

func TestPropertyMatchValidationRejectsBrokenRules(t *testing.T) {
	def := Definition{Steps: Arrange([]Step{
		{StatusKey: "implement", Handler: Handler{Type: HandlerNone}},
		{StatusKey: "code_review", Handler: Handler{Type: HandlerNone}},
	})}
	cases := map[string]Handler{
		"missing property": {Type: HandlerPropertyMatch, Cases: []PropertyMatchCase{{Value: testOptBackend, Handler: Handler{Type: HandlerAgent, ID: testAgentID}}}},
		"empty cases":      {Type: HandlerPropertyMatch, PropertyID: testPropID},
		"duplicate value": {Type: HandlerPropertyMatch, PropertyID: testPropID, Cases: []PropertyMatchCase{
			{Value: testOptBackend, Handler: Handler{Type: HandlerAgent, ID: testAgentID}},
			{Value: testOptBackend, Handler: Handler{Type: HandlerAgent, ID: testAgentFront}},
		}},
		"nested route": {Type: HandlerPropertyMatch, PropertyID: testPropID, Cases: []PropertyMatchCase{
			{Value: testOptBackend, Handler: Handler{Type: HandlerPropertyMatch, PropertyID: testPropID}},
		}},
		"none branch": {Type: HandlerPropertyMatch, PropertyID: testPropID, Cases: []PropertyMatchCase{
			{Value: testOptBackend, Handler: Handler{Type: HandlerNone}},
		}},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateHandoff(NormalizeHandoff(Step{StatusKey: "implement", Handler: handler}), def)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMatchPropertyCaseExact(t *testing.T) {
	step := propertyMatchStep()
	got, err := MatchPropertyCase(step, map[string]any{testPropID: testOptFrontend})
	if err != nil || got.Type != HandlerAgent || got.ID != testAgentFront {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	_, err = MatchPropertyCase(step, map[string]any{})
	var route *RouteError
	if !errors.As(err, &route) || route.Code != RoutePropertyMissing {
		t.Fatalf("missing property: %v", err)
	}
	_, err = MatchPropertyCase(step, map[string]any{testPropID: "unknown-option"})
	if !errors.As(err, &route) || route.Code != RouteNoMatch {
		t.Fatalf("no match: %v", err)
	}
	_, err = MatchPropertyCase(step, map[string]any{testPropID: []any{testOptBackend}})
	if !errors.As(err, &route) || route.Code != RoutePropertyMissing {
		t.Fatalf("non-string value: %v", err)
	}
}

func TestNormalizePropertyMatchDropsStaleFields(t *testing.T) {
	s := NormalizeHandoff(Step{
		StatusKey: "implement",
		Handler: Handler{
			Type:       HandlerPropertyMatch,
			ID:         testAgentID,
			StatusKey:  "todo",
			PropertyID: " " + testPropID + " ",
			Cases: []PropertyMatchCase{{
				Value:   " " + testOptBackend + " ",
				Handler: Handler{Type: HandlerAgent, ID: " " + testAgentID + " ", PropertyID: "x", Cases: []PropertyMatchCase{{Value: "y"}}},
			}},
		},
	})
	if s.Handler.ID != "" || s.Handler.StatusKey != "" || s.Handler.PropertyID != testPropID {
		t.Fatalf("outer fields: %+v", s.Handler)
	}
	c := s.Handler.Cases[0]
	if c.Value != testOptBackend || c.Handler.ID != testAgentID || c.Handler.PropertyID != "" || len(c.Handler.Cases) != 0 {
		t.Fatalf("case fields: %+v", c)
	}
	if !HandlersEqual(s.Handler, s.Handler) || HandlersEqual(s.Handler, Handler{Type: HandlerAgent, ID: testAgentID}) {
		t.Fatal("HandlersEqual mismatch")
	}
}
