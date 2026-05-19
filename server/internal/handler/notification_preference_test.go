package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidatePreferences(t *testing.T) {
	tests := []struct {
		name    string
		prefs   map[string]any
		wantErr bool
	}{
		{
			name:    "valid flat key",
			prefs:   map[string]any{"assignments": "all"},
			wantErr: false,
		},
		{
			name:    "valid flat key muted",
			prefs:   map[string]any{"comments": "muted"},
			wantErr: false,
		},
		{
			name:    "invalid flat key",
			prefs:   map[string]any{"invalid": "all"},
			wantErr: true,
		},
		{
			name:    "invalid flat value",
			prefs:   map[string]any{"assignments": "bad"},
			wantErr: true,
		},
		{
			name:    "flat value wrong type",
			prefs:   map[string]any{"assignments": 123},
			wantErr: true,
		},
		{
			name: "valid channel feishu",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"issues":   true,
						"comments": false,
						"mentions": true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid channel feishu partial",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"issues": true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid channel slack",
			prefs: map[string]any{
				"channel": map[string]any{
					"slack": map[string]any{
						"issues": true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid channel name",
			prefs: map[string]any{
				"channel": map[string]any{
					"Slack!": map[string]any{},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid feishu key",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"invalid_key": true,
					},
				},
			},
			wantErr: true,
		},
		{
			// C3: feishu value must be bool — string is not accepted.
			name: "feishu value wrong type string",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"issues": "yes",
					},
				},
			},
			wantErr: true,
		},
		{
			// C3: feishu value must be bool — number is not accepted.
			name: "feishu value wrong type number",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"comments": 1,
					},
				},
			},
			wantErr: true,
		},
		{
			// C3: feishu value must be bool — nested object is not accepted.
			name: "feishu value wrong type object",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"mentions": map[string]any{"x": 1},
					},
				},
			},
			wantErr: true,
		},
		{
			name:    "channel not an object",
			prefs:   map[string]any{"channel": "bad"},
			wantErr: true,
		},
		{
			name: "feishu not an object",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": "bad",
				},
			},
			wantErr: true,
		},
		{
			name: "valid feishu slash_aliases",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"issues":   true,
						"comments": false,
						"mentions": true,
						"slash_aliases": map[string]any{
							"finish": "done {issue_key}",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "slash_aliases wrong type",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"issues":        true,
						"slash_aliases": "bad",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "slash_aliases value not string",
			prefs: map[string]any{
				"channel": map[string]any{
					"feishu": map[string]any{
						"issues": true,
						"slash_aliases": map[string]any{
							"x": 1,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name:    "empty map",
			prefs:   map[string]any{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePreferences(tt.prefs)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePreferences() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergePreferences(t *testing.T) {
	t.Run("merge into empty", func(t *testing.T) {
		existing := map[string]any{}
		incoming := map[string]any{"assignments": "muted"}
		merged := mergePreferences(existing, incoming)
		if merged["assignments"] != "muted" {
			t.Fatalf("expected assignments=muted, got %v", merged["assignments"])
		}
	})

	t.Run("overwrite flat key", func(t *testing.T) {
		existing := map[string]any{"assignments": "all", "comments": "muted"}
		incoming := map[string]any{"assignments": "muted"}
		merged := mergePreferences(existing, incoming)
		if merged["assignments"] != "muted" {
			t.Fatalf("expected assignments=muted, got %v", merged["assignments"])
		}
		if merged["comments"] != "muted" {
			t.Fatalf("expected comments preserved, got %v", merged["comments"])
		}
	})

	t.Run("merge channel deep", func(t *testing.T) {
		existing := map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"issues":   true,
					"comments": true,
				},
			},
		}
		incoming := map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"comments": false,
				},
			},
		}
		merged := mergePreferences(existing, incoming)
		channel := merged["channel"].(map[string]any)
		feishu := channel["feishu"].(map[string]any)
		if feishu["issues"] != true {
			t.Fatalf("expected issues preserved=true, got %v", feishu["issues"])
		}
		if feishu["comments"] != false {
			t.Fatalf("expected comments=false, got %v", feishu["comments"])
		}
	})

	t.Run("merge slash_aliases deep", func(t *testing.T) {
		existing := map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"issues": true,
					"slash_aliases": map[string]any{
						"a": "done {issue_key}",
					},
				},
			},
		}
		incoming := map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"slash_aliases": map[string]any{
						"b": "status {issue_key} {status}",
					},
				},
			},
		}
		merged := mergePreferences(existing, incoming)
		feishu := merged["channel"].(map[string]any)["feishu"].(map[string]any)
		aliases := feishu["slash_aliases"].(map[string]any)
		if aliases["a"] != "done {issue_key}" || aliases["b"] != "status {issue_key} {status}" {
			t.Fatalf("slash_aliases merge = %v", aliases)
		}
		if feishu["issues"] != true {
			t.Fatalf("expected issues preserved")
		}
	})

	// R2: mergePreferences must not mutate the existing nested maps (defensive copy).
	t.Run("does not mutate existing nested maps", func(t *testing.T) {
		existingFeishu := map[string]any{
			"issues":   true,
			"comments": true,
		}
		existingChannel := map[string]any{
			"feishu": existingFeishu,
		}
		existing := map[string]any{
			"channel": existingChannel,
		}
		incoming := map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"comments": false,
				},
			},
		}
		_ = mergePreferences(existing, incoming)
		// existing.channel.feishu.comments should still be true.
		if existingFeishu["comments"] != true {
			t.Fatalf("existing feishu.comments mutated, expected true got %v", existingFeishu["comments"])
		}
		if _, ok := existingChannel["feishu"].(map[string]any); !ok {
			t.Fatalf("existing channel.feishu mutated to non-map: %T", existingChannel["feishu"])
		}
	})

	t.Run("merge flat and channel together", func(t *testing.T) {
		existing := map[string]any{
			"assignments": "all",
			"channel": map[string]any{
				"feishu": map[string]any{
					"issues": true,
				},
			},
		}
		incoming := map[string]any{
			"comments": "muted",
			"channel": map[string]any{
				"feishu": map[string]any{
					"mentions": false,
				},
			},
		}
		merged := mergePreferences(existing, incoming)
		if merged["assignments"] != "all" {
			t.Fatalf("expected assignments preserved")
		}
		if merged["comments"] != "muted" {
			t.Fatalf("expected comments=muted")
		}
		channel := merged["channel"].(map[string]any)
		feishu := channel["feishu"].(map[string]any)
		if feishu["issues"] != true {
			t.Fatalf("expected issues preserved")
		}
		if feishu["mentions"] != false {
			t.Fatalf("expected mentions=false")
		}
	})
}

func TestNotificationPreferences(t *testing.T) {
	// Helper: get current preferences
	getPrefs := func(t *testing.T) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/notification-preferences", nil)
		testHandler.GetNotificationPreferences(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetNotificationPreferences: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		return resp
	}

	// Helper: update preferences
	updatePrefs := func(t *testing.T, prefs map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/notification-preferences", map[string]any{
			"preferences": prefs,
		})
		testHandler.UpdateNotificationPreferences(w, req)
		return w
	}

	t.Run("GetDefaultEmpty", func(t *testing.T) {
		resp := getPrefs(t)
		if resp["workspace_id"] == nil {
			t.Fatal("expected workspace_id in response")
		}
	})

	t.Run("UpdateFlatKeys", func(t *testing.T) {
		w := updatePrefs(t, map[string]any{
			"assignments": "muted",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateNotificationPreferences: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		prefs := resp["preferences"].(map[string]any)
		if prefs["assignments"] != "muted" {
			t.Fatalf("expected assignments=muted, got %v", prefs["assignments"])
		}
	})

	t.Run("UpdateChannelFeishu", func(t *testing.T) {
		w := updatePrefs(t, map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"issues":   true,
					"comments": false,
					"mentions": true,
				},
			},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateNotificationPreferences: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		prefs := resp["preferences"].(map[string]any)
		channel, ok := prefs["channel"].(map[string]any)
		if !ok {
			t.Fatalf("expected channel to be an object, got %T", prefs["channel"])
		}
		feishu, ok := channel["feishu"].(map[string]any)
		if !ok {
			t.Fatalf("expected feishu to be an object, got %T", channel["feishu"])
		}
		if feishu["comments"] != false {
			t.Fatalf("expected feishu.comments=false, got %v", feishu["comments"])
		}

		// Rec-1: TC-out-3 round-trip — read it back via GET and confirm
		// the stored value survives a fresh fetch (proves PUT actually
		// persisted, not just echoed back the request body).
		got := getPrefs(t)
		gotPrefs := got["preferences"].(map[string]any)
		gotChannel := gotPrefs["channel"].(map[string]any)
		gotFeishu := gotChannel["feishu"].(map[string]any)
		if gotFeishu["comments"] != false {
			t.Fatalf("round-trip GET: expected feishu.comments=false, got %v", gotFeishu["comments"])
		}
	})

	// C3: API must reject non-bool values under channel.feishu so non-bool
	// data never reaches the JSONB column (T13 Subscriber expects bool).
	t.Run("RejectFeishuWrongValueType", func(t *testing.T) {
		w := updatePrefs(t, map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"issues": "banana",
				},
			},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-bool feishu value, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("MergePartialUpdate", func(t *testing.T) {
		// Set initial flat key
		updatePrefs(t, map[string]any{
			"assignments": "all",
		})

		// Update only channel — flat keys should be preserved
		w := updatePrefs(t, map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"issues": true,
				},
			},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateNotificationPreferences: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		resp := getPrefs(t)
		prefs := resp["preferences"].(map[string]any)

		// Flat key should still be present (merged, not replaced)
		if prefs["assignments"] != "all" {
			t.Fatalf("expected assignments=all after merge, got %v", prefs["assignments"])
		}

		// Channel key should be present
		channel, ok := prefs["channel"].(map[string]any)
		if !ok {
			t.Fatal("expected channel object after merge")
		}
		feishu := channel["feishu"].(map[string]any)
		if feishu["issues"] != true {
			t.Fatalf("expected feishu.issues=true, got %v", feishu["issues"])
		}
	})

	t.Run("RejectInvalidFlatKey", func(t *testing.T) {
		w := updatePrefs(t, map[string]any{
			"invalid_key": "all",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid key, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("RejectInvalidFlatValue", func(t *testing.T) {
		w := updatePrefs(t, map[string]any{
			"assignments": "invalid_value",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid value, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("AcceptChannelKey", func(t *testing.T) {
		w := updatePrefs(t, map[string]any{
			"channel": map[string]any{
				"feishu": map[string]any{
					"issues": false,
				},
			},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for channel key, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("AcceptUUIDChannelKey", func(t *testing.T) {
		connectionID := "123e4567-e89b-12d3-a456-426614174000"
		w := updatePrefs(t, map[string]any{
			"channel": map[string]any{
				connectionID: map[string]any{
					"comments": false,
				},
			},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for UUID channel key, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("RejectInvalidChannelKeys", func(t *testing.T) {
		for _, key := range []string{"", "bad key", "bad.key"} {
			w := updatePrefs(t, map[string]any{
				"channel": map[string]any{
					key: map[string]any{
						"issues": false,
					},
				},
			})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for channel key %q, got %d: %s", key, w.Code, w.Body.String())
			}
		}
	})
}
