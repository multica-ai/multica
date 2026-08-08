package handler

// wecom_install_error_matrix_test.go — the install response matrix is a
// contract with two clients at once: an HTTP status the API caller branches on,
// and a stable `code` the settings tab translates. Neither is visible to the
// tests one layer down. installation_params_test.go proves the sentinel
// matching inside the wecom package, and the frontend test mocks the code it
// expects to receive — so between them sits the wiring, where the sentinel is
// turned into a status and a code, and nothing was asserting that.
//
// That gap is exactly where a merge goes wrong: this switch has now taken new
// arms from two directions (the credential probe's rejected/unverifiable pair,
// and this branch's stable codes), and a resolution that drops an arm or gives
// two outcomes the same code still compiles and still passes every other test
// in the tree. The table below is the matrix itself, so a dropped arm fails
// here instead of reaching an admin as the wrong next action.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/wecom"
)

// TestWecomInstallErrorMatrix pins status + error + code for every error class
// the install path can produce.
func TestWecomInstallErrorMatrix(t *testing.T) {
	wsUUID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	agentUUID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		// wantError is the exact sentence, not a smoke check that one exists.
		// It is what a client too old to know the codes shows the admin, so it
		// is as much a part of the contract as the status: an outcome that
		// keeps its status and code but picks up a neighbour's sentence still
		// sends those clients to do the wrong thing, and only pinning the text
		// catches that.
		wantError string
		// why records the admin action this outcome is telling them to take.
		// Two rows that share a code cannot be telling them different things.
		why string
	}{
		{
			name:       "bot owned by another agent in this workspace",
			err:        wecom.ErrBotOwnedBySameWorkspace,
			wantStatus: http.StatusConflict,
			wantCode:   "wecom_bot_owned_by_same_workspace",
			wantError:  "this bot is already connected to another agent in this workspace — disconnect it there first, then connect it here",
			why:        "go disconnect it from the other agent here",
		},
		{
			name:       "bot owned by an archived agent",
			err:        wecom.ErrBotOwnedByArchivedAgent,
			wantStatus: http.StatusConflict,
			wantCode:   "wecom_bot_owned_by_archived_agent",
			wantError:  "this bot is connected to an archived agent in this workspace — restore that agent, or disconnect its bot, before connecting it here",
			why:        "restore that agent or disconnect its bot",
		},
		{
			name:       "bot owned by another workspace",
			err:        wecom.ErrBotOwnedByAnotherWorkspace,
			wantStatus: http.StatusConflict,
			wantCode:   "wecom_bot_owned_by_another_workspace",
			wantError:  "this bot is already connected to a different Multica workspace — disconnect it there before connecting it here",
			why:        "go disconnect it in the other workspace",
		},
		{
			name:       "required field missing",
			err:        fmt.Errorf("%w: bot_id is required", wecom.ErrInvalidInstallationParams),
			wantStatus: http.StatusBadRequest,
			wantCode:   "wecom_install_rejected",
			wantError:  "could not connect the WeCom bot — check the Bot ID and secret from the WeCom admin console, and that the bot is a smart bot with the long connection enabled",
			why:        "fill in the field you left out",
		},
		{
			name:       "WeCom refused the credentials",
			err:        wecom.ErrCredentialsRejected,
			wantStatus: http.StatusBadRequest,
			wantCode:   "wecom_credentials_rejected",
			wantError:  "WeCom rejected this Bot ID and secret — check both on the WeCom admin console, and that the bot is a smart bot with the long connection enabled",
			why:        "go check the pair on the WeCom console — the only outcome that may say this",
		},
		{
			name:       "WeCom unreachable, credentials unverified",
			err:        wecom.ErrCredentialsUnverifiable,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "wecom_credentials_unverifiable",
			wantError:  "could not reach WeCom to verify this bot — the credentials were not changed; try again in a moment",
			why:        "wait and retry; the credentials were not changed",
		},
		{
			name:       "our database is down",
			err:        errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "wecom_install_failed",
			wantError:  "could not save this bot — something went wrong on our side. Your credentials were not changed; please try again, and contact support if it keeps failing",
			why:        "nothing — it is ours to fix, and the admin must not be sent to rotate a secret",
		},
		{
			name:       "the encryption key is missing",
			err:        errors.New("wecom: encrypt secret: secretbox: no key"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "wecom_install_failed",
			wantError:  "could not save this bot — something went wrong on our side. Your credentials were not changed; please try again, and contact support if it keeps failing",
			why:        "nothing — ours",
		},
		{
			name:       "a wrapped sentinel is still classified by its cause",
			err:        fmt.Errorf("upsert: %w", wecom.ErrCredentialsUnverifiable),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "wecom_credentials_unverifiable",
			wantError:  "could not reach WeCom to verify this bot — the credentials were not changed; try again in a moment",
			why:        "errors.Is, not string matching",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeWecomInstallError(w, tc.err, wsUUID, agentUUID)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (%s)", w.Code, tc.wantStatus, tc.why)
			}

			var body struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("response body is not JSON: %v (body: %s)", err, w.Body.String())
			}
			if body.Code != tc.wantCode {
				t.Errorf("code = %q, want %q (%s)", body.Code, tc.wantCode, tc.why)
			}
			// The sentence is what a client that does not know the code shows,
			// so it has to be this outcome's own sentence — an empty one leaves
			// those clients a bare status, and a neighbour's tells the admin to
			// take the wrong action.
			if body.Error != tc.wantError {
				t.Errorf("error = %q, want %q (%s)", body.Error, tc.wantError, tc.why)
			}
		})
	}
}

// TestWecomInstallCodesAreDistinct: the codes are the contract the frontend
// switches on, so two outcomes sharing one code means the tab cannot tell them
// apart and shows the wrong next action for one of them. In particular
// wecom_install_rejected means "you left a field out" and must not widen to
// cover "WeCom said no" or "we could not ask WeCom" — those are different
// actions, and #6594 made them separately detectable for that reason.
func TestWecomInstallCodesAreDistinct(t *testing.T) {
	wsUUID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	agentUUID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	errs := map[string]error{
		"same workspace":   wecom.ErrBotOwnedBySameWorkspace,
		"archived agent":   wecom.ErrBotOwnedByArchivedAgent,
		"other workspace":  wecom.ErrBotOwnedByAnotherWorkspace,
		"invalid params":   wecom.ErrInvalidInstallationParams,
		"creds rejected":   wecom.ErrCredentialsRejected,
		"creds unverified": wecom.ErrCredentialsUnverifiable,
		"ours":             errors.New("something we broke"),
	}

	seen := make(map[string]string, len(errs))
	for name, err := range errs {
		w := httptest.NewRecorder()
		writeWecomInstallError(w, err, wsUUID, agentUUID)

		var body struct {
			Code string `json:"code"`
		}
		if uerr := json.Unmarshal(w.Body.Bytes(), &body); uerr != nil {
			t.Fatalf("%s: response body is not JSON: %v", name, uerr)
		}
		if body.Code == "" {
			t.Errorf("%s: no code — the settings tab falls back to the English sentence", name)
			continue
		}
		if prev, dup := seen[body.Code]; dup {
			t.Errorf("%q is returned for both %q and %q; the frontend cannot tell them apart, so one of the two gets the wrong next action",
				body.Code, prev, name)
			continue
		}
		seen[body.Code] = name
	}
}
