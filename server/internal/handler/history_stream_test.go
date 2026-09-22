package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestWriteHistoryCompleteAndEmpty(t *testing.T) {
	for _, count := range []int{0, 1, int(historyBatchSize), int(historyBatchSize)*3 + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			next := 0
			w := httptest.NewRecorder()
			writeHistory(w, httptest.NewRequest("GET", "/history", nil), "failed", func() ([]int, bool, error) {
				page := []int{}
				for len(page) < int(historyBatchSize) && next < count {
					page = append(page, next)
					next++
				}
				return page, len(page) == int(historyBatchSize), nil
			})
			var got []int
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := make([]int, count)
			for i := range want {
				want[i] = i
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			if w.Code != http.StatusOK {
				t.Fatal(w.Code)
			}
			if count < int(historyBatchSize) && w.Header().Get("Content-Length") == "" {
				t.Fatal("small response lost Content-Length")
			}
			if count >= int(historyBatchSize) && w.Header().Get("Content-Length") != "" {
				t.Fatal("stream advertised a partial Content-Length")
			}
		})
	}
}

func TestWriteHistoryErrorsAndCancellation(t *testing.T) {
	t.Run("first read returns JSON error", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeHistory(w, httptest.NewRequest("GET", "/history", nil), "read failed", func() ([]int, bool, error) { return nil, false, errors.New("database down") })
		if w.Code != 500 || !strings.Contains(w.Body.String(), "read failed") {
			t.Fatal(w.Body.String())
		}
	})
	for _, scenario := range []string{"later_read", "encode", "cancel", "write"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest("GET", "/history", nil).WithContext(ctx)
			recorder := httptest.NewRecorder()
			var w http.ResponseWriter = recorder
			if scenario == "write" {
				w = failingHistoryWriter{recorder}
			}
			calls := 0
			defer func() {
				if got := recover(); got != http.ErrAbortHandler {
					t.Fatalf("panic = %v, want ErrAbortHandler", got)
				}
				if json.Valid(recorder.Body.Bytes()) {
					t.Fatal("partial response looked like complete JSON")
				}
				if scenario == "cancel" && calls != 1 {
					t.Fatalf("queried after cancellation: %d", calls)
				}
			}()
			writeHistory(w, req, "failed", func() ([]any, bool, error) {
				calls++
				if calls > 1 {
					return nil, false, errors.New("late read failure")
				}
				if scenario == "cancel" {
					cancel()
				}
				if scenario == "encode" {
					return []any{make(chan int)}, true, nil
				}
				return []any{strings.Repeat("x", 64000)}, true, nil
			})
			t.Fatal("stream error did not abort")
		})
	}
}

type failingHistoryWriter struct{ *httptest.ResponseRecorder }

func (w failingHistoryWriter) Write(p []byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestWriteHistoryTransportFailureIsNotSuccessfulJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls := 0
		writeHistory(w, r, "failed", func() ([]string, bool, error) {
			calls++
			if calls == 1 {
				return []string{strings.Repeat("x", 65536)}, true, nil
			}
			return nil, false, errors.New("later page failed")
		})
	}))
	defer server.Close()
	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rows []string
	if err := json.NewDecoder(resp.Body).Decode(&rows); err == nil {
		t.Fatal("HTTP client accepted partial history")
	}
}

type historyBoundaryWriter struct {
	*httptest.ResponseRecorder
	calls, failAt int
	cancel        context.CancelFunc
}

func (w *historyBoundaryWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.cancel != nil {
		w.cancel()
	}
	if w.calls == w.failAt {
		return 0, errors.New("write boundary failure")
	}
	return w.ResponseRecorder.Write(p)
}
func TestWriteHistoryFailureBoundaries(t *testing.T) {
	for _, scenario := range []string{"separator_flush", "closing_flush", "final_flush", "cancel_between_pages"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := &historyBoundaryWriter{ResponseRecorder: httptest.NewRecorder(), failAt: 2}
			if scenario == "separator_flush" {
				w.failAt = 1
			}
			if scenario == "cancel_between_pages" {
				w.failAt = 0
				w.cancel = cancel
			}
			loads := 0
			defer func() {
				if got := recover(); got != http.ErrAbortHandler {
					t.Fatalf("panic=%v", got)
				}
				if scenario == "cancel_between_pages" && loads != 1 {
					t.Fatal("read another page after cancellation")
				}
				if json.Valid(w.Body.Bytes()) {
					t.Fatal("accepted truncated JSON")
				}
			}()
			writeHistory(w, httptest.NewRequest("GET", "/history", nil).WithContext(ctx), "failed", func() ([]string, bool, error) {
				loads++
				if scenario == "separator_flush" {
					return []string{strings.Repeat("x", 32764), "tail"}, true, nil
				}
				if loads == 1 {
					return []string{"first"}, true, nil
				}
				if scenario == "closing_flush" {
					return []string{strings.Repeat("x", 32764)}, false, nil
				}
				return []string{"last"}, false, nil
			})
			t.Fatal("failure not propagated")
		})
	}
}
