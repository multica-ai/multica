package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestListLabels_PaginatesAndReturnsAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		switch page {
		case "1":
			w.Header().Set("Link", `<`+srvURLFromRequest(r)+`/api/v4/projects/7/labels?page=2>; rel="next"`)
			json.NewEncoder(w).Encode([]Label{
				{ID: 1, Name: "bug", Color: "#ff0000"},
				{ID: 2, Name: "status::todo", Color: "#888888"},
			})
		case "2":
			json.NewEncoder(w).Encode([]Label{
				{ID: 3, Name: "priority::high", Color: "#ff8800"},
			})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	labels, err := c.ListLabels(context.Background(), "tok", 7)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 3 {
		t.Errorf("got %d labels, want 3", len(labels))
	}
}

func TestCreateLabel_PostsCorrectBody(t *testing.T) {
	var got CreateLabelInput
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/labels" {
			t.Errorf("path = %s", r.URL.Path)
		}
		mu.Lock()
		json.NewDecoder(r.Body).Decode(&got)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Label{ID: 99, Name: got.Name, Color: got.Color, Description: got.Description})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	out, err := c.CreateLabel(context.Background(), "tok", 7, CreateLabelInput{
		Name: "status::todo", Color: "#cccccc", Description: "Multica status",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.Name != "status::todo" || got.Color != "#cccccc" || got.Description != "Multica status" {
		t.Errorf("server received %+v", got)
	}
	if out.ID != 99 {
		t.Errorf("returned ID = %d, want 99", out.ID)
	}
}
