package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGenerateDeviceUserCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := generateDeviceUserCode()
		if err != nil {
			t.Fatalf("generateDeviceUserCode: %v", err)
		}
		if len(code) != deviceUserCodeLength {
			t.Fatalf("code %q: want %d chars, got %d", code, deviceUserCodeLength, len(code))
		}
		for _, c := range code {
			if !strings.ContainsRune(deviceUserCodeAlphabet, c) {
				t.Fatalf("code %q: char %q outside alphabet", code, c)
			}
		}
		seen[code] = true
	}
	if len(seen) < 90 {
		t.Fatalf("codes look non-random: %d unique out of 100", len(seen))
	}
}

func TestNormalizeDeviceUserCode(t *testing.T) {
	cases := map[string]string{
		"ABCD1234":   "ABCD1234",
		"abcd1234":   "ABCD1234",
		"abcd-1234":  "ABCD1234",
		" abcd 1234": "ABCD1234",
		"AbCd-1-2-3-4": "ABCD1234",
		"":           "",
		"----":       "",
	}
	for in, want := range cases {
		if got := normalizeDeviceUserCode(in); got != want {
			t.Errorf("normalizeDeviceUserCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatDeviceUserCode(t *testing.T) {
	if got := formatDeviceUserCode("ABCD1234"); got != "ABCD-1234" {
		t.Errorf("formatDeviceUserCode = %q, want ABCD-1234", got)
	}
	if got := formatDeviceUserCode("short"); got != "short" {
		t.Errorf("formatDeviceUserCode passthrough = %q", got)
	}
}

func TestDeviceVerificationURL(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://multica.example.com:6433/")
	if got := deviceVerificationURL(); got != "https://multica.example.com:6433/activate" {
		t.Errorf("deviceVerificationURL = %q", got)
	}
	t.Setenv("MULTICA_APP_URL", "")
	if got := deviceVerificationURL(); got != "" {
		t.Errorf("deviceVerificationURL with empty env = %q, want empty", got)
	}
}

// deviceMockDB routes QueryRow by SQL shape so the DeviceToken handler can be
// exercised against every authorization state without a database.
type deviceMockDB struct {
	db.DBTX
	row db.DeviceAuthorization // returned by the SELECT
	err error                  // returned by the SELECT (e.g. pgx.ErrNoRows)
}

func (m *deviceMockDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return &deviceRow{row: m.row, err: m.err, sql: sql}
}

func (m *deviceMockDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type deviceRow struct {
	row db.DeviceAuthorization
	err error
	sql string
}

func (r *deviceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch {
	case strings.Contains(r.sql, "SET consumed_at"):
		if dest[0] != nil {
			if p, ok := dest[0].(*pgtype.Text); ok {
				*p = r.row.Token
			}
		}
		return nil
	default:
		da := r.row
		set := func(d any, v any) {
			switch p := d.(type) {
			case *pgtype.UUID:
				*p = v.(pgtype.UUID)
			case *string:
				*p = v.(string)
			case *pgtype.Text:
				*p = v.(pgtype.Text)
			case *pgtype.Timestamptz:
				*p = v.(pgtype.Timestamptz)
			case *int32:
				*p = v.(int32)
			}
		}
		set(dest[0], da.ID)
		set(dest[1], da.DeviceCode)
		set(dest[2], da.UserCodeHash)
		set(dest[3], da.UserID)
		set(dest[4], da.Status)
		set(dest[5], da.Token)
		set(dest[6], da.ExpiresAt)
		set(dest[7], da.IntervalSeconds)
		set(dest[8], da.LastPolledAt)
		set(dest[9], da.ApprovedAt)
		set(dest[10], da.ConsumedAt)
		set(dest[11], da.CreatedAt)
		return nil
	}
}

func TestDeviceTokenStates(t *testing.T) {
	now := time.Now()
	ts := func(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
	base := db.DeviceAuthorization{
		ID:              pgtype.UUID{Valid: true},
		DeviceCode:      "dev",
		UserCodeHash:    "hash",
		Status:          "pending",
		ExpiresAt:       ts(now.Add(5 * time.Minute)),
		IntervalSeconds: 5,
	}

	tests := []struct {
		name      string
		row       db.DeviceAuthorization
		selectErr error
		wantCode  int
		wantErr   string
	}{
		{"unknown device code", base, pgx.ErrNoRows, http.StatusBadRequest, deviceErrInvalid},
		{"pending", base, nil, http.StatusBadRequest, deviceErrPending},
		{"pending and expired", func() db.DeviceAuthorization {
			r := base
			r.ExpiresAt = ts(now.Add(-time.Minute))
			return r
		}(), nil, http.StatusBadRequest, deviceErrExpired},
		{"polling too fast", func() db.DeviceAuthorization {
			r := base
			r.LastPolledAt = ts(now.Add(-1 * time.Second))
			return r
		}(), nil, http.StatusBadRequest, deviceErrSlow},
		{"denied", func() db.DeviceAuthorization {
			r := base
			r.Status = "denied"
			return r
		}(), nil, http.StatusBadRequest, deviceErrDenied},
		{"approved, token collected", func() db.DeviceAuthorization {
			r := base
			r.Status = "approved"
			r.ConsumedAt = ts(now)
			return r
		}(), nil, http.StatusBadRequest, deviceErrInvalid},
		{"approved, token ready", func() db.DeviceAuthorization {
			r := base
			r.Status = "approved"
			r.Token = pgtype.Text{String: "jwt-value", Valid: true}
			return r
		}(), nil, http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(Config{})
			h.Queries = db.New(&deviceMockDB{row: tt.row, err: tt.selectErr})

			body := `{"device_code":"dev"}`
			req := httptest.NewRequest(http.MethodPost, "/auth/device/token", strings.NewReader(body))
			w := httptest.NewRecorder()
			h.DeviceToken(w, req)

			if w.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tt.wantCode, w.Body.String())
			}
			var resp DeviceTokenResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if tt.wantErr == "" {
				if resp.Token != "jwt-value" {
					t.Fatalf("token = %q, want jwt-value", resp.Token)
				}
			} else if resp.Error != tt.wantErr {
				t.Fatalf("error = %q, want %q", resp.Error, tt.wantErr)
			}
		})
	}
}

func TestApproveDeviceAuthorizationInvalidCode(t *testing.T) {
	h := newTestHandler(Config{})
	h.Queries = db.New(&deviceMockDB{err: pgx.ErrNoRows})

	req := httptest.NewRequest(http.MethodPost, "/api/device/approve", strings.NewReader(`{"user_code":"ABCD-1234"}`))
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000000")
	w := httptest.NewRecorder()
	h.ApproveDeviceAuthorization(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid or expired") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestApproveDeviceAuthorizationRequiresAuth(t *testing.T) {
	h := newTestHandler(Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/device/approve", strings.NewReader(`{"user_code":"ABCD-1234"}`))
	w := httptest.NewRecorder()
	h.ApproveDeviceAuthorization(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
