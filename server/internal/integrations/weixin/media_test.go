package weixin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func TestInboundMediaWithoutStorageIsExplicitlyUnavailable(t *testing.T) {
	var msg message
	if err := json.Unmarshal([]byte(`{"message_id":1,"from_user_id":"owner","message_type":1,"item_list":[{"type":2,"image_item":{}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	in, ok := normalizeMessage(credentials{installConfig: installConfig{AppID: "bot", UserID: "owner"}}, msg)
	if !ok {
		t.Fatal("image message was rejected")
	}
	resolver := NewMediaResolver(nil, nil)
	got := resolver.ResolveMedia(context.Background(), engine.ResolvedInstallation{}, engine.ResolvedIdentity{}, pgtype.UUID{}, pgtype.UUID{}, in)
	if len(got.MediaRefs) != 0 || !strings.Contains(got.Text, "WeChat attachment unavailable") {
		t.Fatal("missing storage must not look like a delivered attachment")
	}
}

func TestMediaCryptoInteroperability(t *testing.T) {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	plain, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	encrypted, err := encryptMedia(plain, key)
	// AES-128 ECB known-answer vector, independent of our decryptor.
	if err != nil || hex.EncodeToString(encrypted[:16]) != "69c4e0d86a7b0430d8cdb78070b4c55a" || len(encrypted) != 32 {
		t.Fatalf("wrong AES vector or padding: %v", err)
	}
	got, err := decryptMedia(encrypted, key)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypt: %v", err)
	}
	for _, encoded := range []string{base64.StdEncoding.EncodeToString(key), base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key)))} {
		got, err := mediaKey(mediaRef{AESKey: encoded}, "")
		if err != nil || !bytes.Equal(got, key) {
			t.Fatalf("key encoding: %v", err)
		}
	}
	if _, err := decryptMedia(encrypted[:17], key); err == nil {
		t.Fatal("accepted unaligned ciphertext")
	}
	if _, err := decryptMedia(encrypted[:16], key); err == nil {
		t.Fatal("accepted invalid padding")
	}
	if _, err := mediaKey(mediaRef{AESKey: "bad"}, ""); err == nil {
		t.Fatal("accepted invalid key")
	}
}

func TestMediaUploadAndDownloadProtocol(t *testing.T) {
	plain := []byte("attachment content")
	var uploadKey, encrypted []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Error("missing upload authorization")
			}
			var body struct {
				Key       string `json:"aeskey"`
				Size      int    `json:"rawsize"`
				MediaType int    `json:"media_type"`
				UserID    string `json:"to_user_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			uploadKey, _ = hex.DecodeString(body.Key)
			if body.Size != len(plain) || body.MediaType != 3 || body.UserID != "owner" {
				t.Error("wrong upload metadata")
			}
			_, _ = w.Write([]byte(`{"ret":0,"upload_full_url":"https://novac2c.cdn.weixin.qq.com/c2c/upload"}`))
		case "/c2c/upload":
			if r.Header.Get("Authorization") != "" || r.Header.Get("AuthorizationType") != "" {
				t.Error("bot credential leaked to CDN")
			}
			encrypted, _ = io.ReadAll(r.Body)
			got, err := decryptMedia(encrypted, uploadKey)
			if err != nil || !bytes.Equal(got, plain) {
				t.Error("uploaded body is not encrypted media")
			}
			w.Header().Set("x-encrypted-param", "opaque +/= download")
		case "/c2c/download":
			if r.URL.Query().Get("encrypted_query_param") != "opaque +/= download" || r.Header.Get("Authorization") != "" {
				t.Error("wrong download request")
			}
			_, _ = w.Write(encrypted)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := providerClient(server)
	item, err := newAPI(DefaultBaseURL, "token", client).Upload(context.Background(), "owner", plain, "../report.txt", "text/plain")
	if err != nil || item.File == nil || item.File.Name != "report.txt" {
		t.Fatalf("upload: %v", err)
	}
	decodedKey, _ := base64.StdEncoding.DecodeString(item.File.Media.AESKey)
	if len(decodedKey) != 32 {
		t.Fatal("outbound AES key must be base64 of hex text")
	}
	got, err := downloadMedia(context.Background(), client, inboundMedia{ref: item.File.Media})
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("download: %v", err)
	}
}

func TestMediaRejectsUntrustedURLsAndOversize(t *testing.T) {
	for _, target := range []string{"http://novac2c.cdn.weixin.qq.com/c2c/download", "https://weixin.qq.com.evil.test/file", "https://127.0.0.1/file", "https://user:pass@cdn.weixin.qq.com/file"} {
		if trustedMediaURL(target) {
			t.Fatalf("accepted %s", target)
		}
	}
	called := false
	client := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { called = true; return nil, ErrProtocol })}
	_, err := newAPI(DefaultBaseURL, "token", client).Upload(context.Background(), "owner", make([]byte, maxMediaBytes+1), "file", "application/octet-stream")
	if err == nil || called {
		t.Fatal("oversize file reached provider")
	}
	_, err = downloadMedia(context.Background(), client, inboundMedia{ref: mediaRef{FullURL: "https://evil.test/", AESKey: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 16)))}})
	if err == nil || called {
		t.Fatal("untrusted media URL reached transport")
	}
}
