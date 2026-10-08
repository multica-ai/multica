package main

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestMaintenanceRoutesAreAbsentFromPublicServer(t *testing.T) {
	for _, path := range []string{"/maintenance/jobs", "/api/maintenance/jobs", "/maintenance/jobs/00000000-0000-0000-0000-000000000001/advance"} {
		req, err := http.NewRequest("POST", testServer.URL+path, bytes.NewBufferString("{}"))
		if err != nil {
			t.Fatal(err)
		}
		// Even an authenticated application user cannot invoke the internal API here.
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
}
