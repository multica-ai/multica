package handler

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type crmIMAPMailboxConfig struct {
	ID            string
	UUID          pgtype.UUID
	Label         string
	Email         string
	Host          string
	Port          int32
	TLSMode       string
	Username      string
	SecretRef     string
	SMTPHost      string
	SMTPPort      int32
	SMTPTLSMode   string
	SMTPUsername  string
	SMTPSecretRef string
	OwnerType     string
	OwnerID       string
}

type crmIMAPFetchedMessage struct {
	UID          string
	MessageID    string
	InReplyTo    string
	ReferenceIDs []string
	RawHeaders   map[string][]string
	Attachments  []crmEmailAttachmentMetadata
	Subject      string
	FromEmail    string
	FromName     string
	ToEmails     []string
	CcEmails     []string
	Date         time.Time
	BodyText     string
	BodyHTML     string
	Snippet      string
	RawSize      int
}

type crmEmailAttachmentMetadata struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int    `json:"size_bytes"`
	Inline      bool   `json:"inline"`
	ContentID   string `json:"content_id,omitempty"`
	Disposition string `json:"disposition,omitempty"`
}

type crmIMAPClient struct {
	conn *textproto.Conn
	tag  int
}

func crmMailboxSecretKey() []byte {
	if raw := strings.TrimSpace(os.Getenv("CRM_MAILBOX_SECRET_KEY")); raw != "" {
		if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
			return decoded
		}
		if len(raw) == 32 {
			return []byte(raw)
		}
	}
	fallback := os.Getenv("JWT_SECRET")
	if fallback == "" {
		fallback = "multica-dev-secret-change-in-production"
	}
	sum := sha256.Sum256([]byte("crm-mailbox-secret:" + fallback))
	return sum[:]
}

func resolveCRMIMAPSecret(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("IMAP password is missing")
	}
	if strings.HasPrefix(ref, "enc:v1:") {
		payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ref, "enc:v1:"))
		if err != nil {
			return "", fmt.Errorf("invalid encrypted IMAP secret")
		}
		block, err := aes.NewCipher(crmMailboxSecretKey())
		if err != nil {
			return "", fmt.Errorf("invalid CRM mailbox secret key")
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return "", fmt.Errorf("invalid CRM mailbox cipher")
		}
		if len(payload) < gcm.NonceSize() {
			return "", fmt.Errorf("invalid encrypted IMAP secret")
		}
		nonce, ciphertext := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return "", fmt.Errorf("invalid encrypted IMAP secret")
		}
		return string(plaintext), nil
	}
	if strings.HasPrefix(ref, "inline:") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ref, "inline:"))
		if err != nil {
			return "", fmt.Errorf("invalid inline IMAP secret")
		}
		return string(decoded), nil
	}
	if strings.HasPrefix(ref, "env:") {
		name := strings.TrimSpace(strings.TrimPrefix(ref, "env:"))
		if name == "" {
			return "", fmt.Errorf("invalid IMAP secret env ref")
		}
		value := os.Getenv(name)
		if value == "" {
			return "", fmt.Errorf("IMAP secret env var is empty")
		}
		return value, nil
	}
	// Backward compatibility for existing rows where secret_ref stored the password directly.
	return ref, nil
}

func encodeCRMIMAPInlineSecret(secret string) string {
	block, err := aes.NewCipher(crmMailboxSecretKey())
	if err != nil {
		slog.Warn("failed to initialize CRM mailbox secret encryption", "error", err)
		return "inline:" + base64.StdEncoding.EncodeToString([]byte(secret))
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		slog.Warn("failed to initialize CRM mailbox secret cipher", "error", err)
		return "inline:" + base64.StdEncoding.EncodeToString([]byte(secret))
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		slog.Warn("failed to generate CRM mailbox secret nonce", "error", err)
		return "inline:" + base64.StdEncoding.EncodeToString([]byte(secret))
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(secret), nil)
	payload := append(nonce, ciphertext...)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(payload)
}

func dialCRMIMAP(cfg crmIMAPMailboxConfig) (*crmIMAPClient, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var c net.Conn
	var err error
	if cfg.TLSMode == "ssl" {
		c, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		c, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	client := &crmIMAPClient{conn: textproto.NewConn(c)}
	if _, err := client.conn.ReadLine(); err != nil {
		_ = client.Close()
		return nil, err
	}
	if cfg.TLSMode == "starttls" {
		if err := client.simple("STARTTLS"); err != nil {
			_ = client.Close()
			return nil, err
		}
		tlsConn := tls.Client(c, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			_ = c.Close()
			return nil, err
		}
		client.conn = textproto.NewConn(tlsConn)
	}
	return client, nil
}

func (c *crmIMAPClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	_ = c.simple("LOGOUT")
	return c.conn.Close()
}

func (c *crmIMAPClient) nextTag() string {
	c.tag++
	return fmt.Sprintf("A%04d", c.tag)
}

func (c *crmIMAPClient) simple(command string, args ...string) error {
	_, err := c.command(command, args...)
	return err
}

func (c *crmIMAPClient) command(command string, args ...string) ([]string, error) {
	tag := c.nextTag()
	parts := append([]string{tag, command}, args...)
	if err := c.conn.PrintfLine("%s", strings.Join(parts, " ")); err != nil {
		return nil, err
	}
	var lines []string
	for {
		line, err := c.conn.ReadLine()
		if err != nil {
			return lines, err
		}
		lines = append(lines, line)
		if strings.HasPrefix(line, tag+" ") {
			upper := strings.ToUpper(line)
			if strings.Contains(upper, " OK") || strings.HasPrefix(upper, tag+" OK") {
				return lines, nil
			}
			return lines, fmt.Errorf("%s", line)
		}
	}
}

func imapQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func fetchCRMIMAPMessages(cfg crmIMAPMailboxConfig, folder string, limit int, rangeDays int, requestedUIDs []string) ([]crmIMAPFetchedMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	folder = strings.TrimSpace(folder)
	if folder == "" || strings.EqualFold(folder, "inbox") {
		folder = "INBOX"
	}
	password, err := resolveCRMIMAPSecret(cfg.SecretRef)
	if err != nil {
		return nil, err
	}
	client, err := dialCRMIMAP(cfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if err := client.simple("LOGIN", imapQuote(cfg.Username), imapQuote(password)); err != nil {
		return nil, fmt.Errorf("IMAP login failed: %w", err)
	}
	if _, err := client.command("SELECT", imapQuote(folder)); err != nil {
		return nil, fmt.Errorf("IMAP select failed: %w", err)
	}
	uids := requestedUIDs
	if len(uids) == 0 {
		args := []string{"ALL"}
		if rangeDays > 0 {
			since := time.Now().AddDate(0, 0, -rangeDays).Format("02-Jan-2006")
			args = []string{"SINCE", since}
		}
		lines, err := client.command("UID SEARCH", args...)
		if err != nil {
			return nil, fmt.Errorf("IMAP search failed: %w", err)
		}
		uids = parseIMAPSearchUIDs(lines)
		if len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}
	}
	if len(uids) == 0 {
		return []crmIMAPFetchedMessage{}, nil
	}
	// newest first for UI preview
	sort.SliceStable(uids, func(i, j int) bool { return atoiSafe(uids[i]) > atoiSafe(uids[j]) })
	messages := make([]crmIMAPFetchedMessage, 0, len(uids))
	for _, uid := range uids {
		lines, err := client.command("UID FETCH", uid, "(UID BODY.PEEK[])")
		if err != nil {
			return messages, err
		}
		raw := extractIMAPLiteral(lines)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		msg := parseCRMIMAPMessage(uid, raw)
		messages = append(messages, msg)
	}
	return messages, nil
}

func parseIMAPSearchUIDs(lines []string) []string {
	for _, line := range lines {
		if strings.HasPrefix(line, "* SEARCH") {
			fields := strings.Fields(strings.TrimPrefix(line, "* SEARCH"))
			return fields
		}
	}
	return nil
}

func extractIMAPLiteral(lines []string) string {
	if len(lines) <= 1 {
		return ""
	}
	var body []string
	for _, line := range lines {
		if strings.HasPrefix(line, "*") || regexp.MustCompile(`^A\d+ `).MatchString(line) {
			continue
		}
		body = append(body, line)
	}
	return strings.Join(body, "\r\n")
}

func parseCRMIMAPMessage(uid, raw string) crmIMAPFetchedMessage {
	msg := crmIMAPFetchedMessage{UID: uid, RawSize: len(raw)}
	parsed, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		msg.BodyText = raw
		msg.Snippet = makeSnippet(raw)
		return msg
	}
	decode := new(mime.WordDecoder).DecodeHeader
	msg.RawHeaders = map[string][]string(parsed.Header)
	msg.MessageID = cleanMessageID(parsed.Header.Get("Message-Id"))
	msg.InReplyTo = cleanMessageID(parsed.Header.Get("In-Reply-To"))
	msg.ReferenceIDs = cleanMessageIDList(parsed.Header.Get("References"))
	if msg.MessageID == "" {
		msg.MessageID = uid
	}
	msg.Subject, _ = decode(parsed.Header.Get("Subject"))
	if froms, err := parsed.Header.AddressList("From"); err == nil && len(froms) > 0 {
		msg.FromEmail = froms[0].Address
		msg.FromName, _ = decode(froms[0].Name)
	}
	msg.ToEmails = headerEmails(parsed.Header, "To")
	msg.CcEmails = headerEmails(parsed.Header, "Cc")
	if date, err := parsed.Header.Date(); err == nil {
		msg.Date = date
	}
	body, _ := io.ReadAll(parsed.Body)
	contentType := parsed.Header.Get("Content-Type")
	msg.BodyText, msg.BodyHTML, msg.Attachments = extractReadableEmailParts(contentType, body)
	if strings.TrimSpace(msg.BodyText) == "" && strings.TrimSpace(msg.BodyHTML) != "" {
		msg.BodyText = htmlToPlainText(msg.BodyHTML)
	}
	if strings.TrimSpace(msg.BodyText) == "" {
		msg.BodyText = string(body)
	}
	msg.Snippet = makeSnippet(msg.BodyText)
	return msg
}

func extractReadableEmailBodies(contentType string, body []byte) (string, string) {
	textBody, htmlBody, _ := extractReadableEmailParts(contentType, body)
	return textBody, htmlBody
}

func extractReadableEmailParts(contentType string, body []byte) (string, string, []crmEmailAttachmentMetadata) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return decodeTransferBody(body, ""), "", nil
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		var textBody, htmlBody string
		var attachments []crmEmailAttachmentMetadata
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			partType := part.Header.Get("Content-Type")
			partBody, _ := io.ReadAll(part)
			pt, partParams, _ := mime.ParseMediaType(partType)
			disposition, dispositionParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			decodedBytes := decodeTransferBytes(partBody, part.Header.Get("Content-Transfer-Encoding"))
			decoded := string(decodedBytes)
			filename := dispositionParams["filename"]
			if filename == "" {
				filename = partParams["name"]
			}
			contentID := strings.Trim(part.Header.Get("Content-ID"), " <>")
			isInline := strings.EqualFold(disposition, "inline") || contentID != ""
			if filename != "" || strings.EqualFold(disposition, "attachment") || (isInline && strings.HasPrefix(strings.ToLower(pt), "image/")) {
				attachments = append(attachments, crmEmailAttachmentMetadata{Filename: filename, ContentType: pt, SizeBytes: len(decodedBytes), Inline: isInline, ContentID: contentID, Disposition: disposition})
				continue
			}
			if strings.HasPrefix(pt, "multipart/") {
				nestedText, nestedHTML, nestedAttachments := extractReadableEmailParts(partType, partBody)
				attachments = append(attachments, nestedAttachments...)
				if textBody == "" {
					textBody = nestedText
				}
				if htmlBody == "" {
					htmlBody = nestedHTML
				}
			} else if strings.EqualFold(pt, "text/plain") && textBody == "" {
				textBody = decoded
			} else if strings.EqualFold(pt, "text/html") && htmlBody == "" {
				htmlBody = decoded
			}
		}
		return textBody, htmlBody, attachments
	}
	decoded := decodeTransferBody(body, "")
	if strings.EqualFold(mediaType, "text/html") {
		return htmlToPlainText(decoded), decoded, nil
	}
	return decoded, "", nil
}

func decodeTransferBody(body []byte, encoding string) string {
	return string(decodeTransferBytes(body, encoding))
}

func decodeTransferBytes(body []byte, encoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(body)), ""))
		if err == nil {
			return decoded
		}
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(bufio.NewReader(bytes.NewReader(body))))
		if err == nil {
			return decoded
		}
	}
	return body
}

func cleanMessageID(value string) string {
	return strings.Trim(strings.TrimSpace(value), " <>")
}

func cleanMessageIDList(value string) []string {
	fields := strings.Fields(value)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if id := cleanMessageID(field); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func crmEmailJSON(value any, fallback string) any {
	b, err := json.Marshal(value)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(b)
}

func htmlToPlainText(value string) string {
	value = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</\1>`).ReplaceAllString(value, " ")
	value = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(value, "\n")
	value = regexp.MustCompile(`(?i)</p>`).ReplaceAllString(value, "\n")
	value = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func headerEmails(header mail.Header, key string) []string {
	addresses, err := header.AddressList(key)
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address.Address != "" {
			out = append(out, address.Address)
		}
	}
	return out
}

func makeSnippet(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if len(body) > 240 {
		return body[:240]
	}
	return body
}

func atoiSafe(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}
