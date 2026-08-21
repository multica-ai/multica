package remotemcptest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestFixtureExposesReadAndSideEffectingWrite(t *testing.T) {
	fixture := NewServer()
	defer fixture.Close()

	call := func(method, params string) map[string]any {
		t.Helper()
		body := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`)
		request, err := http.NewRequest(http.MethodPost, fixture.URL, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+Credential)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var decoded map[string]any
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}

	listed := call("tools/list", `{}`)
	if listed["result"] == nil {
		t.Fatalf("tools/list = %#v", listed)
	}
	call("tools/call", `{"name":"fixture.write","arguments":{"value":"one"}}`)
	if writes := fixture.Writes(); len(writes) != 1 || writes[0] != "one" {
		t.Fatalf("writes = %#v", writes)
	}
}
