package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func catalogPages(t *testing.T, pages ...string) (RequestFunc, *int) {
	t.Helper()
	calls := 0
	return func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		if method != "model/list" {
			t.Fatalf("method = %s", method)
		}
		if calls >= len(pages) {
			t.Fatal("asked for more pages than exist")
		}
		calls++
		return json.RawMessage(pages[calls-1]), nil
	}, &calls
}

func TestCatalogDefaultModelReadsLaterPagesAndEitherIdentity(t *testing.T) {
	t.Parallel()

	request, calls := catalogPages(t, `{"data":[{"model":"a"}],"nextCursor":"n"}`, `{"data":[{"model":"b","isDefault":true}],"nextCursor":null}`)
	if got, err := CatalogDefaultModel(context.Background(), request); err != nil || got != "b" || *calls != 2 {
		t.Fatalf("got %q, %v after %d calls", got, err, *calls)
	}
	request, _ = catalogPages(t, `{"data":[{"id":"from-id","isDefault":true}]}`)
	if got, err := CatalogDefaultModel(context.Background(), request); err != nil || got != "from-id" {
		t.Fatalf("id-only row = %q, %v", got, err)
	}
}

func TestCatalogDefaultModelRefusesWhatItCannotDecide(t *testing.T) {
	t.Parallel()

	for name, pages := range map[string][]string{
		"ambiguous":  {`{"data":[{"model":"a","isDefault":true},{"model":"b","isDefault":true}]}`},
		"none":       {`{"data":[{"model":"a"}]}`},
		"malformed":  {`{}`},
		"cursorLoop": {`{"data":[],"nextCursor":"x"}`, `{"data":[],"nextCursor":"x"}`},
	} {
		request, _ := catalogPages(t, pages...)
		if got, err := CatalogDefaultModel(context.Background(), request); err == nil {
			t.Errorf("%s: resolved %q", name, got)
		}
	}
}

func TestCatalogDefaultModelKeepsTheRequestError(t *testing.T) {
	t.Parallel()

	boom := errors.New("closed")
	_, err := CatalogDefaultModel(context.Background(), func(context.Context, string, any) (json.RawMessage, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
