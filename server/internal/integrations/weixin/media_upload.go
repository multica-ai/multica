package weixin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5" // iLink's upload contract requires the raw file's MD5.
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Uploads from our servers to the WeChat CDN were measured at ~20 KB/s, so a
// fixed deadline sized for API calls cut off every file above ~0.8 MB. The
// budget scales with the file, floored below that measured rate, and is capped
// so one file cannot hold a conversation's reply queue indefinitely.
const (
	mediaTransferBase    = 45 * time.Second
	mediaTransferMinRate = 16 << 10 // bytes per second
	mediaTransferMax     = 10 * time.Minute

	attachmentReadTimeout = 45 * time.Second
)

func mediaTransferTimeout(size int64) time.Duration {
	return min(mediaTransferBase+time.Duration(size)*time.Second/mediaTransferMinRate, mediaTransferMax)
}

// cdnTransport carries CDN uploads over HTTP/1.1, as the provider's reference
// client does. Over HTTP/2 the CDN grants a 64 KiB stream window, and uploads
// ran far below what the same network path achieves to the iLink API.
var cdnTransport http.RoundTripper = http1Transport(http.DefaultTransport.(*http.Transport))

func http1Transport(base *http.Transport) *http.Transport {
	tr := base.Clone()
	if tr.TLSClientConfig != nil {
		tr.TLSClientConfig.NextProtos = nil // else ALPN can still select h2
	}
	tr.Protocols = new(http.Protocols)
	tr.Protocols.SetHTTP1(true)
	return tr
}

// cdnClient is bounded by the caller's size-scaled deadline, not by the API
// client's per-request timeout.
func (c *apiClient) cdnClient() *http.Client {
	client := *c.http
	client.Timeout = 0
	if client.Transport == nil {
		client.Transport = cdnTransport
	}
	return &client
}

func encryptMedia(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil || len(key) != aes.BlockSize {
		return nil, ErrProtocol
	}
	padding := aes.BlockSize - len(data)%aes.BlockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(padded[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return padded, nil
}

func (c *apiClient) Upload(ctx context.Context, userID string, data []byte, filename, contentType string) (messageItem, error) {
	if len(data) > maxMediaBytes {
		return messageItem{}, errors.New("weixin: file exceeds the 20 MiB transfer limit")
	}
	ctx, cancel := context.WithTimeout(ctx, mediaTransferTimeout(int64(len(data))))
	defer cancel()
	key := make([]byte, aes.BlockSize)
	fileKey := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return messageItem{}, err
	}
	if _, err := rand.Read(fileKey); err != nil {
		return messageItem{}, err
	}
	encrypted, err := encryptMedia(data, key)
	if err != nil {
		return messageItem{}, err
	}
	mediaType := 3 // unknown formats and audio are delivered as ordinary files
	if contentType == "image/jpeg" || contentType == "image/png" || contentType == "image/gif" || contentType == "image/webp" {
		mediaType = 1
	}
	if contentType == "video/mp4" {
		mediaType = 2
	}
	var result struct {
		apiResult
		URL   string `json:"upload_full_url"`
		Param string `json:"upload_param"`
	}
	checksum := md5.Sum(data)
	if err := c.request(ctx, http.MethodPost, "getuploadurl", map[string]any{
		"filekey": hex.EncodeToString(fileKey), "media_type": mediaType, "to_user_id": userID,
		"rawsize": len(data), "rawfilemd5": hex.EncodeToString(checksum[:]), "filesize": len(encrypted),
		"no_need_thumb": true, "aeskey": hex.EncodeToString(key), "base_info": baseInfo(),
	}, true, &result); err != nil {
		return messageItem{}, err
	}
	if err := result.check(false); err != nil {
		return messageItem{}, err
	}
	target := result.URL
	if target == "" && result.Param != "" {
		target = defaultCDN + "/upload?" + url.Values{"encrypted_query_param": {result.Param}, "filekey": {hex.EncodeToString(fileKey)}}.Encode()
	}
	if !trustedMediaURL(target) {
		return messageItem{}, ErrProtocol
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encrypted))
	if err != nil {
		return messageItem{}, ErrProtocol
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.cdnClient().Do(req) // no Authorization on the CDN request
	if err != nil {
		if ctx.Err() != nil {
			return messageItem{}, fmt.Errorf("weixin: media upload of %d bytes: %w", len(encrypted), ctx.Err())
		}
		// The URL carries the provider's upload ticket; keep it out of logs.
		return messageItem{}, errors.New("weixin: media upload failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return messageItem{}, &ProviderError{Status: resp.StatusCode}
	}
	query := resp.Header.Get("x-encrypted-param")
	if query == "" {
		return messageItem{}, ErrProtocol
	}
	ref := mediaRef{Query: query, AESKey: base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key))), EncryptType: 1}
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if filename == "" || filename == "." || filename == "/" {
		filename = "attachment"
	}
	item := messageItem{}
	switch mediaType {
	case 1:
		item.Type = 2
		item.Image = &struct {
			Media  mediaRef `json:"media"`
			AESKey string   `json:"aeskey,omitempty"`
			Size   int64    `json:"mid_size,omitempty"`
		}{Media: ref, Size: int64(len(encrypted))}
	case 2:
		item.Type = 5
		item.Video = &struct {
			Media mediaRef `json:"media"`
			Size  int64    `json:"video_size,omitempty"`
		}{Media: ref, Size: int64(len(encrypted))}
	default:
		item.Type = 4
		item.File = &struct {
			Media  mediaRef `json:"media"`
			Name   string   `json:"file_name"`
			Length string   `json:"len,omitempty"`
		}{Media: ref, Name: filename, Length: strconv.Itoa(len(data))}
	}
	return item, nil
}

func (o *Outbound) EnableFileDelivery(store objectStore) { o.objects = store }

func (o *Outbound) attachment(ctx context.Context, api *apiClient, userID string, row db.Attachment) (messageItem, error) {
	if row.SizeBytes > maxMediaBytes {
		return messageItem{}, errors.New("weixin: file exceeds the 20 MiB transfer limit")
	}
	key := o.objects.KeyFromURL(row.Url)
	if key == "" {
		return messageItem{}, errors.New("weixin: attachment is not in Multica object storage")
	}
	data, err := o.readAttachment(ctx, key)
	if err != nil {
		return messageItem{}, err
	}
	// Upload applies its own size-scaled deadline for the slow CDN leg.
	return api.Upload(ctx, userID, data, row.Filename, row.ContentType)
}

// readAttachment keeps the fixed object-storage deadline: a stalled read must
// not hold the conversation's reply queue for the CDN upload's longer budget.
func (o *Outbound) readAttachment(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, attachmentReadTimeout)
	defer cancel()
	reader, err := o.objects.GetReader(ctx, key)
	if err != nil {
		return nil, errors.New("weixin: cannot read attachment")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxMediaBytes+1))
	if err != nil || len(data) > maxMediaBytes {
		return nil, errors.New("weixin: attachment transfer failed or exceeds limit")
	}
	return data, nil
}
