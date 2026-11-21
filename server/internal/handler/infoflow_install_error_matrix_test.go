package handler

// infoflow_install_error_matrix_test.go — the same contract the WeCom matrix
// pins, for InfoFlow: an HTTP status the API caller branches on AND a stable
// `code` the settings dialog translates. The installer decides the action the
// admin takes next, so an outcome that keeps its status but picks up a
// neighbour's sentence still sends them to do the wrong thing. A dropped arm in
// writeInfoFlowInstallError still compiles and still passes every other test in
// the tree, so the table below is the matrix itself.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/infoflow"
)

// TestInfoFlowInstallErrorMatrix pins status + error + code for every error
// class the InfoFlow install path can produce.
func TestInfoFlowInstallErrorMatrix(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		// wantError is the exact sentence a client too old to know the codes
		// shows the admin.
		wantError string
		// why records the admin action this outcome tells them to take. Two
		// rows sharing a code could not be telling them different things.
		why string
	}{
		{
			name:       "app owned by another agent in this workspace",
			err:        infoflow.ErrAppOwnedBySameWorkspace,
			wantStatus: http.StatusConflict,
			wantCode:   "infoflow_app_owned_by_same_workspace",
			wantError:  "this InfoFlow app is already connected to another agent in this workspace — disconnect it there first, then connect it here",
			why:        "go disconnect it from the other agent here",
		},
		{
			name:       "app owned by an archived agent",
			err:        infoflow.ErrAppOwnedByArchivedAgent,
			wantStatus: http.StatusConflict,
			wantCode:   "infoflow_app_owned_by_archived_agent",
			wantError:  "this InfoFlow app is connected to an archived agent in this workspace — restore that agent, or disconnect its app, before connecting it here",
			why:        "restore that agent or disconnect its app",
		},
		{
			name:       "app owned by another workspace",
			err:        infoflow.ErrAppOwnedByAnotherWorkspace,
			wantStatus: http.StatusConflict,
			wantCode:   "infoflow_app_owned_by_another_workspace",
			wantError:  "this InfoFlow app is already connected to a different Multica workspace — disconnect it there before connecting it here",
			why:        "go disconnect it in the other workspace",
		},
		{
			name:       "app key missing",
			err:        infoflow.ErrInvalidAppKey,
			wantStatus: http.StatusBadRequest,
			wantCode:   "infoflow_install_rejected",
			wantError:  "infoflow: AppKey is required",
			why:        "fill in the App Key field",
		},
		{
			name:       "app secret missing",
			err:        infoflow.ErrInvalidAppSecret,
			wantStatus: http.StatusBadRequest,
			wantCode:   "infoflow_install_rejected",
			wantError:  "infoflow: AppSecret is required",
			why:        "fill in the App Secret field",
		},
		{
			name:       "our database is down",
			err:        errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "infoflow_install_failed",
			wantError:  "could not connect the InfoFlow bot — something went wrong on our side. Your credentials were not changed; try again, and contact support if it keeps failing",
			why:        "nothing — it is ours to fix",
		},
		{
			name:       "the encryption key is missing",
			err:        errors.New("infoflow: encrypt infoflow app secret: secretbox: no key"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "infoflow_install_failed",
			wantError:  "could not connect the InfoFlow bot — something went wrong on our side. Your credentials were not changed; try again, and contact support if it keeps failing",
			why:        "nothing — ours",
		},
		{
			name:       "a wrapped sentinel is still classified by its cause",
			err:        fmt.Errorf("upsert: %w", infoflow.ErrAppOwnedByAnotherWorkspace),
			wantStatus: http.StatusConflict,
			wantCode:   "infoflow_app_owned_by_another_workspace",
			wantError:  "this InfoFlow app is already connected to a different Multica workspace — disconnect it there before connecting it here",
			why:        "errors.Is, not string matching",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeInfoFlowInstallError(w, tc.err)

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
			if body.Error != tc.wantError {
				t.Errorf("error = %q, want %q (%s)", body.Error, tc.wantError, tc.why)
			}
		})
	}
}

// TestInfoFlowInstallCodesAreDistinct: the codes are the contract the settings
// dialog switches on, so two outcomes sharing one code means the tab cannot tell
// them apart and shows the wrong next action for one of them. "You left a field
// out" must not widen to cover "the app belongs to someone else".
func TestInfoFlowInstallCodesAreDistinct(t *testing.T) {
	errs := map[string]error{
		"same workspace":  infoflow.ErrAppOwnedBySameWorkspace,
		"archived agent":  infoflow.ErrAppOwnedByArchivedAgent,
		"other workspace": infoflow.ErrAppOwnedByAnotherWorkspace,
		"invalid params":  infoflow.ErrInvalidAppKey,
		"ours":            errors.New("something we broke"),
	}

	seen := make(map[string]string, len(errs))
	for name, err := range errs {
		w := httptest.NewRecorder()
		writeInfoFlowInstallError(w, err)

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
