package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestTokenizeQueryUsesPinnedParserVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/tokenize" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer parser-secret" {
			t.Errorf("authorization header = %q", request.Header.Get("Authorization"))
		}
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload["text"] != "你好 world" {
			t.Errorf("request text = %q", payload["text"])
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(tokenizerResponse{
			Tokens:           []string{"你好", "world", "world"},
			TokenizerVersion: TokenizerVersion,
		})
	}))
	defer server.Close()

	service := &Service{parserURL: server.URL, parserToken: "parser-secret", parserClient: server.Client()}
	got, err := service.tokenizeQuery(context.Background(), "你好 world")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"你好", "world"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
}

func TestTokenizeQueryFallsBackWhenParserVersionChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(tokenizerResponse{
			Tokens:           []string{"错误版本"},
			TokenizerVersion: "old-tokenizer",
		})
	}))
	defer server.Close()

	service := &Service{parserURL: server.URL, parserClient: server.Client()}
	got, err := service.tokenizeQuery(context.Background(), "你好")
	if err == nil {
		t.Fatal("version mismatch did not return an error")
	}
	want := SearchTokens("你好")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback tokens = %#v, want %#v", got, want)
	}
}

func TestTokenizeQueryUsesLocalFallbackWithoutParser(t *testing.T) {
	service := &Service{}
	got, err := service.tokenizeQuery(context.Background(), "你好 World")
	if err != nil {
		t.Fatal(err)
	}
	want := SearchTokens("你好 World")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback tokens = %#v, want %#v", got, want)
	}
}

func TestRenderVisionPagesValidatesAndDecodesParserImages(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4e, 0x47}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/render" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer parser-secret" {
			t.Errorf("authorization header = %q", request.Header.Get("Authorization"))
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart request: %v", err)
		}
		var metadata struct {
			Pages []int `json:"pages"`
		}
		if err := json.Unmarshal([]byte(request.FormValue("metadata")), &metadata); err != nil {
			t.Errorf("decode metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata.Pages, []int{2}) {
			t.Errorf("requested pages = %#v", metadata.Pages)
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(parserRenderResponse{Images: []parserRenderedImage{{Page: 2, MIMEType: "image/png", DataBase64: base64.StdEncoding.EncodeToString(imageData)}}})
	}))
	defer server.Close()

	service := &Service{parserURL: server.URL, parserToken: "parser-secret", parserClient: server.Client(), maxUpload: 1 << 20}
	got, err := service.renderVisionPages(context.Background(), []byte("%PDF-1.7"), "source.pdf", "application/pdf", []int{2, 2})
	if err != nil {
		t.Fatal(err)
	}
	image, ok := got[2]
	if !ok || image.MIMEType != "image/png" || !reflect.DeepEqual(image.Data, imageData) {
		t.Fatalf("decoded images = %#v", got)
	}
}
