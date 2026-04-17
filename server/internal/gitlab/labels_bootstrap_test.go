package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

func TestBootstrapScopedLabels_CreatesMissingOnly(t *testing.T) {
	var mu sync.Mutex
	existing := map[string]bool{
		"status::todo": true,
		"bug":          true,
	}
	created := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			out := []gitlabapi.Label{}
			mu.Lock()
			for n := range existing {
				out = append(out, gitlabapi.Label{ID: int64(len(out) + 1), Name: n})
			}
			mu.Unlock()
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var in gitlabapi.CreateLabelInput
			json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			created[in.Name] = true
			existing[in.Name] = true
			mu.Unlock()
			json.NewEncoder(w).Encode(gitlabapi.Label{ID: 999, Name: in.Name, Color: in.Color})
		}
	}))
	defer srv.Close()

	c := gitlabapi.NewClient(srv.URL, srv.Client())
	if err := BootstrapScopedLabels(context.Background(), c, "tok", 7); err != nil {
		t.Fatalf("BootstrapScopedLabels: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, name := range CanonicalScopedLabelNames() {
		if name == "status::todo" {
			if created[name] {
				t.Errorf("re-created pre-existing label %q", name)
			}
			continue
		}
		if !created[name] {
			t.Errorf("did not create missing label %q", name)
		}
	}
}
