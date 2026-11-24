package issueworkflow

import "testing"

func stepsEqualLocal(a, b Step) bool {
	return a.StatusKey == b.StatusKey &&
		a.Instructions == b.Instructions &&
		a.NextStatusKey == b.NextStatusKey &&
		a.BackStatusKey == b.BackStatusKey &&
		HandlersEqual(a.Handler, b.Handler)
}

func TestDynamicHandlerValidationAndRoundTrip(t *testing.T) {
	def := devDefinition()
	for _, kind := range []string{HandlerCurrentAssignee, HandlerPreviousStepAssignee} {
		s := NormalizeHandoff(Step{StatusKey: "implement", Handler: Handler{Type: kind, ID: testAgentID, StatusKey: "implement"}, Instructions: " do it ", NextStatusKey: "done"})
		if err := ValidateHandoff(s, def); err != nil {
			t.Fatal(err)
		}
		if s.Handler.ID != "" || s.Instructions != "do it" || !s.HandsOff() {
			t.Fatalf("normalized=%+v", s)
		}
		encoded, err := EncodeSteps([]Step{s})
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeSteps(encoded)
		if err != nil || len(decoded) != 1 || !stepsEqualLocal(decoded[0], s) {
			t.Fatalf("roundtrip=%+v err=%v", decoded, err)
		}
	}
	for _, key := range []string{"", "other-workflow-stage"} {
		if err := ValidateHandoff(Step{StatusKey: "implement", Handler: Handler{Type: HandlerPreviousStepAssignee, StatusKey: key}}, def); err == nil {
			t.Fatalf("accepted reference %q", key)
		}
	}
}
