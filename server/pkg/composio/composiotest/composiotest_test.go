package composiotest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/composio/composiotest"
)

func TestExampleMatchesTheSpecForEveryOperation(t *testing.T) {
	ops := composiotest.Operations()
	if len(ops) < 12 {
		t.Fatalf("snapshot lists %d operations: %v", len(ops), ops)
	}
	for _, op := range ops {
		body := composiotest.Example(t, op, nil)
		if len(body) == 0 {
			t.Errorf("%s: empty example", op)
		}
	}
}

func TestExampleReturnsACopy(t *testing.T) {
	for op, edit := range map[string]func(map[string]any){
		"POST /connected_accounts/{nanoid}/revoke": func(body map[string]any) {
			body["connected_account"].(map[string]any)["id"] = "edited"
		},
		"GET /toolkits": func(body map[string]any) {
			meta := body["items"].([]any)[0].(map[string]any)["meta"].(map[string]any)
			meta["categories"].([]any)[0].(map[string]any)["name"] = "edited"
		},
	} {
		body := composiotest.Example(t, op, nil)
		before, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		edit(body)
		after, err := json.Marshal(composiotest.Example(t, op, nil))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("%s: editing one example changed the next:\n before %s\n after  %s", op, before, after)
		}
	}
}

func wantProblem(t *testing.T, err error, problem string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), problem) {
		t.Errorf("error = %v, want it to report %q", err, problem)
	}
}

// The two shapes that hid MUL-9920's bugs.
func TestCheckRejectsFieldsComposioDoesNotReturn(t *testing.T) {
	listed := composiotest.Example(t, "GET /toolkits", nil)
	listed["items"].([]any)[0].(map[string]any)["categories"] = []any{"Developer Tools"}
	wantProblem(t, composiotest.Check("GET /toolkits", 200, listed), "$.items[0].categories: is not in the spec")

	details := composiotest.Example(t, "GET /toolkits/{slug}", nil)
	details["auth_schemes"] = []any{"api_key"}
	wantProblem(t, composiotest.Check("GET /toolkits/{slug}", 200, details), "$.auth_schemes: is not in the spec")
}

func TestCheckReportsTypesRequiredFieldsAndEnums(t *testing.T) {
	body := composiotest.Example(t, "GET /connected_accounts", nil)
	account := body["items"].([]any)[0].(map[string]any)
	delete(account, "user_id")
	account["status"] = "CONNECTED"
	account["is_disabled"] = "no"
	err := composiotest.Check("GET /connected_accounts", 200, body)
	wantProblem(t, err, "$.items[0]: misses required field user_id")
	wantProblem(t, err, `$.items[0].status: "CONNECTED" is not one of`)
	wantProblem(t, err, "$.items[0].is_disabled: is string, the spec says boolean")
}

func TestCheckFieldsSkipsUnsetGoFields(t *testing.T) {
	type toolkit struct {
		Slug        string   `json:"slug"`
		Name        string   `json:"name"`
		AuthSchemes []string `json:"auth_schemes"`
		NoAuth      bool     `json:"no_auth"`
	}
	if err := composiotest.CheckFields("GET /toolkits/{slug}", toolkit{Slug: "notion"}); err != nil {
		t.Errorf("unset fields reported: %v", err)
	}
	wantProblem(t, composiotest.CheckFields("GET /toolkits/{slug}", toolkit{Slug: "notion", AuthSchemes: []string{"oauth2"}}),
		"$.auth_schemes: is not in the spec")
}

func TestResponseMergesOverlayOntoTheSpec(t *testing.T) {
	body := composiotest.Example(t, "GET /toolkits/{slug}", map[string]any{
		"slug": "openai",
		"auth_config_details": []any{
			map[string]any{"mode": "API_KEY"},
			map[string]any{"mode": "BASIC", "fields": map[string]any{"auth_config_creation": map[string]any{"required": []any{}}}},
		},
	})
	details := body["auth_config_details"].([]any)
	if body["slug"] != "openai" || len(details) != 2 {
		t.Fatalf("overlay not applied: %v", body)
	}
	basic := details[1].(map[string]any)
	if basic["mode"] != "BASIC" || basic["name"] == nil || basic["is_connection_revoke_supported"] == nil {
		t.Errorf("item not completed from the spec: %v", basic)
	}

	errBody := composiotest.Response(t, "GET /toolkits/{slug}", 404, map[string]any{"error": map[string]any{"message": "nope"}})
	if errBody["error"].(map[string]any)["message"] != "nope" {
		t.Errorf("error body = %v", errBody)
	}
}
