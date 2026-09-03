package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type observedDoneContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return nil
}

func botIdentityInstallation(t *testing.T, appKey, robotCode string) db.ChannelInstallation {
	t.Helper()
	config, err := json.Marshal(installConfig{
		AppID:              appKey,
		RobotCode:          robotCode,
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("app-secret")),
	})
	if err != nil {
		t.Fatalf("marshal installation config: %v", err)
	}
	return db.ChannelInstallation{Config: config}
}

func TestNewBotNameResolverDefaultsClient(t *testing.T) {
	resolver := NewBotNameResolver(nil, nil)
	if resolver.client == nil || resolver.client.apiBase != defaultAPIBase {
		t.Fatalf("default client = %#v, want DingTalk API client", resolver.client)
	}
}

func TestBotNameInGroupSelectsExactRobotIdentityAndRefreshes401(t *testing.T) {
	mints := 0
	groupCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case accessTokenPath:
			mints++
			_, _ = fmt.Fprintf(w, `{"accessToken":"tok-%d","expireIn":7200}`, mints)
		case groupBotsPath:
			groupCalls++
			if groupCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("x-acs-dingtalk-access-token"); got != "tok-2" {
				t.Errorf("refreshed token = %q, want tok-2", got)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode group bot request: %v", err)
			}
			if body["openConversationId"] != "cid-platform" {
				t.Errorf("request body = %v", body)
			}
			_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"robot-support","name":"Support Bot"},{"robotCode":"robot-release","name":" Release Bot "}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	name, err := NewClient(nil, srv.URL).botNameInGroup(
		context.Background(), "app-key", "app-secret", "robot-release", "cid-platform",
	)
	if err != nil || name != "Release Bot" {
		t.Fatalf("botNameInGroup = %q, %v", name, err)
	}
	if mints != 2 || groupCalls != 2 {
		t.Fatalf("token/group calls = %d/%d, want 2/2", mints, groupCalls)
	}
}

func TestBotNameInGroupRejectsInvalidAndMissingIdentities(t *testing.T) {
	client := NewClient(nil, "")
	if _, err := client.botNameInGroup(context.Background(), "app", "secret", "", "cid"); err == nil {
		t.Fatal("empty robot code must fail before a request")
	}
	if _, err := client.botNameInGroup(context.Background(), "app", "secret", "robot", ""); err == nil {
		t.Fatal("empty conversation id must fail before a request")
	}

	tests := []struct {
		name string
		body string
	}{
		{name: "robot absent", body: `{"chatbotInstanceVOList":[{"robotCode":"other","name":"Other"}]}`},
		{name: "blank name", body: `{"chatbotInstanceVOList":[{"robotCode":"robot","name":"  "}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == accessTokenPath {
					_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
					return
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			if _, err := NewClient(nil, srv.URL).botNameInGroup(
				context.Background(), "app", "secret", "robot", "cid",
			); err == nil {
				t.Fatal("expected identity lookup to fail")
			}
		})
	}
}

func TestBotNameInGroupClassifiesOnlyChatManagePermission(t *testing.T) {
	for _, tt := range []struct {
		name       string
		message    string
		classified bool
	}{
		{name: "chat manage", message: "missing qyapi_chat_manage", classified: true},
		{name: "other permission", message: "missing another_permission", classified: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == accessTokenPath {
					_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
					return
				}
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprintf(w, `{"code":%q,"message":%q}`, dingTalkPermissionDeniedCode, tt.message)
			}))
			defer srv.Close()
			_, err := NewClient(nil, srv.URL).botNameInGroup(
				context.Background(), "app", "secret", "robot", "cid",
			)
			if errors.Is(err, errMissingChatManagePermission) != tt.classified {
				t.Fatalf("error = %v, classified=%t", err, tt.classified)
			}
		})
	}
}

func TestBotNameResolverCachesSuccessfulIdentityAcrossGroups(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		calls.Add(1)
		_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"release","name":"Release"},{"robotCode":"support","name":"Support"}]}`))
	}))
	defer srv.Close()

	resolver := NewBotNameResolver(NewClient(nil, srv.URL), nil)
	for range 2 {
		if name, err := resolver.Resolve(context.Background(), botIdentityInstallation(t, "app", "release"), "group-a"); err != nil || name != "Release" {
			t.Fatalf("cached release = %q, %v", name, err)
		}
	}
	if _, err := resolver.Resolve(context.Background(), botIdentityInstallation(t, "app", "support"), "group-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), botIdentityInstallation(t, "app", "release"), "group-b"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("API calls = %d, want one per Bot", got)
	}
}

func TestBotNameResolverSharesCacheBetweenChannelCredentialsAndInstallation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		calls.Add(1)
		_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"release","name":"Release Bot"}]}`))
	}))
	defer srv.Close()

	resolver := NewBotNameResolver(NewClient(nil, srv.URL), nil)
	name, err := resolver.resolveCredentials(context.Background(), credentials{
		AppKey: "app", AppSecret: "secret", RobotCode: "release",
	}, "group-a")
	if err != nil || name != "Release Bot" {
		t.Fatalf("channel credential resolution = %q, %v", name, err)
	}
	name, err = resolver.Resolve(context.Background(), botIdentityInstallation(t, "app", "release"), "group-b")
	if err != nil || name != "Release Bot" {
		t.Fatalf("installation resolution = %q, %v", name, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shared resolver API calls = %d, want 1", got)
	}
}

func TestBotNameResolverCachesTransientAndBrieflyCachesPermissionFailure(t *testing.T) {
	groupCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		groupCalls++
		switch groupCalls {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"unavailable","message":"retry"}`))
		case 2:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"Forbidden.AccessDenied.AccessTokenPermissionDenied","message":"missing qyapi_chat_manage"}`))
		default:
			_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"release","name":"Release"}]}`))
		}
	}))
	defer srv.Close()

	resolver := NewBotNameResolver(NewClient(nil, srv.URL), nil)
	now := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }
	installation := botIdentityInstallation(t, "app", "release")
	for range 2 {
		if _, err := resolver.Resolve(context.Background(), installation, "group"); err == nil {
			t.Fatal("transient failure expected")
		}
	}
	if groupCalls != 1 {
		t.Fatalf("transient API calls = %d, want cached failure", groupCalls)
	}
	now = now.Add(botNameErrorCacheTTL + time.Second)
	if _, err := resolver.Resolve(context.Background(), installation, "group"); !errors.Is(err, errMissingChatManagePermission) {
		t.Fatalf("permission error = %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), installation, "another-group"); !errors.Is(err, errMissingChatManagePermission) {
		t.Fatalf("cached cross-group permission error = %v", err)
	}
	if groupCalls != 2 {
		t.Fatalf("permission denial API calls = %d, want one app-scoped lookup", groupCalls)
	}
	now = now.Add(botNamePermissionCacheTTL + time.Second)
	name, err := resolver.Resolve(context.Background(), installation, "group")
	if err != nil || name != "Release" || groupCalls != 3 {
		t.Fatalf("permission retry after TTL = %q, %v (calls %d)", name, err, groupCalls)
	}
}

func TestBotNameResolverRefreshesSuccessfulNameAfterTTL(t *testing.T) {
	groupCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		groupCalls++
		name := "Release Bot"
		if groupCalls > 1 {
			name = "Release Bot Renamed"
		}
		_, _ = fmt.Fprintf(w, `{"chatbotInstanceVOList":[{"robotCode":"release","name":%q}]}`, name)
	}))
	defer srv.Close()

	resolver := NewBotNameResolver(NewClient(nil, srv.URL), nil)
	now := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }
	installation := botIdentityInstallation(t, "app", "release")
	name, err := resolver.Resolve(context.Background(), installation, "group")
	if err != nil || name != "Release Bot" {
		t.Fatalf("initial Resolve = %q, %v", name, err)
	}
	now = now.Add(botNameCacheTTL - time.Second)
	name, err = resolver.Resolve(context.Background(), installation, "group")
	if err != nil || name != "Release Bot" || groupCalls != 1 {
		t.Fatalf("Resolve before TTL = %q, %v (calls %d)", name, err, groupCalls)
	}
	now = now.Add(2 * time.Second)
	name, err = resolver.Resolve(context.Background(), installation, "group")
	if err != nil || name != "Release Bot Renamed" || groupCalls != 2 {
		t.Fatalf("Resolve after TTL = %q, %v (calls %d)", name, err, groupCalls)
	}
}

func TestBotNameResolverTimesOutDetachedLookup(t *testing.T) {
	groupStarted := make(chan struct{})
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == accessTokenPath {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"accessToken":"tok","expireIn":7200}`)),
			}, nil
		}
		close(groupStarted)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	resolver := NewBotNameResolver(NewClient(httpClient, "https://api.example.test"), nil)
	resolver.timeout = 10 * time.Millisecond
	_, err := resolver.Resolve(
		context.Background(),
		botIdentityInstallation(t, "app", "release"),
		"group",
	)
	<-groupStarted
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out Resolve error = %v, want context deadline exceeded", err)
	}
}

func TestBotNameResolverCallerCancellationDoesNotCancelSharedLookup(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case accessTokenPath:
			close(started)
			<-release
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"accessToken":"tok","expireIn":7200}`)),
			}, nil
		case groupBotsPath:
			close(finished)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"chatbotInstanceVOList":[{"robotCode":"release","name":"Release Bot"}]}`)),
			}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}, nil
		}
	})}
	resolver := NewBotNameResolver(NewClient(httpClient, "https://api.example.test"), nil)
	installation := botIdentityInstallation(t, "app", "release")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(ctx, installation, "group")
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation error = %v, want context.Canceled", err)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("detached shared lookup did not finish after caller cancellation")
	}
}

func TestBotNameResolverCollapsesLookupsAndBoundsCache(t *testing.T) {
	var groupCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		if groupCalls.Add(1) == 1 {
			close(started)
		}
		if r.Header.Get("x-test-block") == "" && r.URL.Path == groupBotsPath && groupCalls.Load() == 1 {
			<-release
		}
		_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"release","name":"Release"}]}`))
	}))
	defer srv.Close()

	resolver := NewBotNameResolver(NewClient(nil, srv.URL), nil)
	resolver.maxSize = 2
	installation := botIdentityInstallation(t, "app", "release")
	results := make(chan error, 2)
	for _, groupID := range []string{"group-a", "group-b"} {
		go func() {
			_, err := resolver.Resolve(context.Background(), installation, groupID)
			results <- err
		}()
	}
	<-started
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := groupCalls.Load(); got != 1 {
		t.Fatalf("concurrent cross-group calls = %d, want 1 per Bot", got)
	}
	resolver.mu.Lock()
	resolver.cache["expired"] = cachedBotName{expiresAt: time.Now().Add(-time.Second)}
	resolver.mu.Unlock()
	for _, appKey := range []string{"app-b", "app-c"} {
		if _, err := resolver.Resolve(
			context.Background(),
			botIdentityInstallation(t, appKey, "release"),
			"group-a",
		); err != nil {
			t.Fatal(err)
		}
	}
	resolver.mu.Lock()
	cacheSize := len(resolver.cache)
	_, keptExpired := resolver.cache["expired"]
	resolver.mu.Unlock()
	if cacheSize != 2 || keptExpired {
		t.Fatalf("cache size/expired entry = %d/%t, want 2/false", cacheSize, keptExpired)
	}
}

func TestBotNameResolverRetriesGroupSpecificFailureForAnotherGroup(t *testing.T) {
	groupAStarted := make(chan struct{})
	releaseGroupA := make(chan struct{})
	var groupCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		groupCalls.Add(1)
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode group bot request: %v", err)
		}
		switch body["openConversationId"] {
		case "group-a":
			close(groupAStarted)
			<-releaseGroupA
			_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"other","name":"Other"}]}`))
		case "group-b":
			_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"release","name":"Release"}]}`))
		default:
			t.Errorf("unexpected conversation id %q", body["openConversationId"])
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	resolver := NewBotNameResolver(NewClient(nil, srv.URL), nil)
	installation := botIdentityInstallation(t, "app", "release")
	type outcome struct {
		name string
		err  error
	}
	groupAResult := make(chan outcome, 1)
	go func() {
		name, err := resolver.Resolve(context.Background(), installation, "group-a")
		groupAResult <- outcome{name: name, err: err}
	}()
	<-groupAStarted

	waiting := make(chan struct{})
	groupBContext := &observedDoneContext{Context: context.Background(), observed: waiting}
	groupBResult := make(chan outcome, 1)
	go func() {
		name, err := resolver.Resolve(groupBContext, installation, "group-b")
		groupBResult <- outcome{name: name, err: err}
	}()
	<-waiting
	close(releaseGroupA)

	if got := <-groupAResult; got.err == nil || !strings.Contains(got.err.Error(), "absent from group") {
		t.Fatalf("group A result = %q, %v; want group-specific absence", got.name, got.err)
	}
	if got := <-groupBResult; got.err != nil || got.name != "Release" {
		t.Fatalf("group B result = %q, %v; want its own successful lookup", got.name, got.err)
	}
	if got := groupCalls.Load(); got != 2 {
		t.Fatalf("group API calls = %d, want one failed group and one successful group", got)
	}
}
