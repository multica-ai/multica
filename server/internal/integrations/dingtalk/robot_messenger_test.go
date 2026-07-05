package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupUserUnionID(t *testing.T) {
	var gotUserID, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_, _ = w.Write([]byte(`{"accessToken":"tok_test","expireIn":7200}`))
		case "/topapi/v2/user/get":
			gotToken = r.URL.Query().Get("access_token")
			var body struct {
				UserID string `json:"userid"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotUserID = body.UserID
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","result":{"unionid":"union_42","org_email":"Dev@Corp.com"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	m := NewRobotMessenger(srv.URL, srv.URL, srv.Client())
	unionID, email, err := m.LookupUserUnionID(context.Background(), channelCredentials{ClientID: "ck", ClientSecret: "cs"}, "staff_9")
	if err != nil {
		t.Fatalf("LookupUserUnionID: %v", err)
	}
	if unionID != "union_42" || email != "Dev@Corp.com" {
		t.Fatalf("unexpected result: %q %q", unionID, email)
	}
	if gotUserID != "staff_9" || gotToken != "tok_test" {
		t.Fatalf("unexpected request: userid=%q token=%q", gotUserID, gotToken)
	}
}

func TestLookupUserUnionIDLegacyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			_, _ = w.Write([]byte(`{"accessToken":"tok_test","expireIn":7200}`))
			return
		}
		// HTTP 200 with a non-zero errcode is still a failure.
		_, _ = w.Write([]byte(`{"errcode":60011,"errmsg":"no permission"}`))
	}))
	t.Cleanup(srv.Close)

	m := NewRobotMessenger(srv.URL, srv.URL, srv.Client())
	if _, _, err := m.LookupUserUnionID(context.Background(), channelCredentials{ClientID: "ck", ClientSecret: "cs"}, "staff_9"); err == nil {
		t.Fatal("expected a legacy envelope error")
	}
}
