package handler

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

type crmEmailAttachment struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
}

type crmEmailSendPayload struct {
	ToEmails     []string             `json:"to_emails"`
	CcEmails     []string             `json:"cc_emails"`
	BccEmails    []string             `json:"bcc_emails"`
	Subject      string               `json:"subject"`
	BodyText     string               `json:"body_text"`
	BodyHTML     string               `json:"body_html"`
	InReplyTo    string               `json:"in_reply_to"`
	ReferenceIDs []string             `json:"reference_ids"`
	Attachments  []crmEmailAttachment `json:"attachments"`
	AppendToSent bool                 `json:"append_to_sent"`
}

func sendCRMSMTP(cfg crmIMAPMailboxConfig, payload crmEmailSendPayload) (string, []byte, error) {
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		return "", nil, fmt.Errorf("SMTP host is required")
	}
	port := cfg.SMTPPort
	if port <= 0 {
		port = 465
	}
	username := strings.TrimSpace(cfg.SMTPUsername)
	if username == "" {
		username = cfg.Username
	}
	secretRef := strings.TrimSpace(cfg.SMTPSecretRef)
	if secretRef == "" {
		secretRef = cfg.SecretRef
	}
	password, err := resolveCRMIMAPSecret(secretRef)
	if err != nil {
		return "", nil, sanitizeCRMSendError(err)
	}
	from := strings.TrimSpace(cfg.Email)
	recipients := append(append(append([]string{}, payload.ToEmails...), payload.CcEmails...), payload.BccEmails...)
	if len(recipients) == 0 {
		return "", nil, fmt.Errorf("recipient is required")
	}
	messageID := newCRMMessageID(cfg.SMTPHost)
	message, err := buildCRMSMTPMessage(from, payload, messageID)
	if err != nil {
		return "", nil, err
	}
	addr := net.JoinHostPort(cfg.SMTPHost, fmt.Sprintf("%d", port))
	auth := smtp.PlainAuth("", username, password, cfg.SMTPHost)
	mode := strings.ToLower(strings.TrimSpace(cfg.SMTPTLSMode))
	if mode == "" || mode == "ssl" {
		err = sendCRMSMTPOverTLS(addr, cfg.SMTPHost, auth, from, recipients, message)
	} else {
		err = smtp.SendMail(addr, auth, from, recipients, message)
	}
	if err != nil {
		return "", nil, sanitizeCRMSendError(err)
	}
	return messageID, message, nil
}

func sendCRMSMTPOverTLS(addr, host string, auth smtp.Auth, from string, recipients []string, message []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Quit()
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(message); err != nil {
		_ = wc.Close()
		return err
	}
	return wc.Close()
}

func buildCRMSMTPMessage(from string, payload crmEmailSendPayload, messageID string) ([]byte, error) {
	var buf bytes.Buffer
	boundary := "multica_" + strings.ReplaceAll(messageID, "@", "_")
	headers := []string{
		"From: " + from,
		"To: " + strings.Join(payload.ToEmails, ", "),
		"Subject: " + strings.ReplaceAll(payload.Subject, "\n", " "),
		"Message-ID: " + messageID,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
	}
	if len(payload.CcEmails) > 0 {
		headers = append(headers[:2], append([]string{"Cc: " + strings.Join(payload.CcEmails, ", ")}, headers[2:]...)...)
	}
	if strings.TrimSpace(payload.InReplyTo) != "" {
		headers = append(headers, "In-Reply-To: "+strings.TrimSpace(payload.InReplyTo))
	}
	if len(payload.ReferenceIDs) > 0 {
		headers = append(headers, "References: "+strings.Join(payload.ReferenceIDs, " "))
	}
	if len(payload.Attachments) == 0 && strings.TrimSpace(payload.BodyHTML) == "" {
		headers = append(headers, "Content-Type: text/plain; charset=UTF-8", "Content-Transfer-Encoding: quoted-printable", "")
		buf.WriteString(strings.Join(headers, "\r\n"))
		qw := quotedprintable.NewWriter(&buf)
		_, _ = qw.Write([]byte(payload.BodyText))
		_ = qw.Close()
		return buf.Bytes(), nil
	}
	headers = append(headers, `Content-Type: multipart/mixed; boundary="`+boundary+`"`, "")
	buf.WriteString(strings.Join(headers, "\r\n"))
	writer := multipart.NewWriter(&buf)
	_ = writer.SetBoundary(boundary)
	bodyHeader := textproto.MIMEHeader{}
	if strings.TrimSpace(payload.BodyHTML) != "" {
		bodyHeader.Set("Content-Type", "text/html; charset=UTF-8")
	} else {
		bodyHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	}
	bodyHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := writer.CreatePart(bodyHeader)
	if err != nil {
		return nil, err
	}
	qw := quotedprintable.NewWriter(part)
	if strings.TrimSpace(payload.BodyHTML) != "" {
		_, _ = qw.Write([]byte(payload.BodyHTML))
	} else {
		_, _ = qw.Write([]byte(payload.BodyText))
	}
	_ = qw.Close()
	for _, attachment := range payload.Attachments {
		data, err := base64.StdEncoding.DecodeString(attachment.Content)
		if err != nil {
			return nil, fmt.Errorf("invalid attachment content for %s", attachment.FileName)
		}
		contentType := attachment.ContentType
		if strings.TrimSpace(contentType) == "" {
			contentType = "application/octet-stream"
		}
		partHeader := textproto.MIMEHeader{}
		partHeader.Set("Content-Type", contentType)
		partHeader.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(attachment.FileName, "\"", "")))
		partHeader.Set("Content-Transfer-Encoding", "base64")
		part, err := writer.CreatePart(partHeader)
		if err != nil {
			return nil, err
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		for len(encoded) > 76 {
			_, _ = part.Write([]byte(encoded[:76] + "\r\n"))
			encoded = encoded[76:]
		}
		_, _ = part.Write([]byte(encoded + "\r\n"))
	}
	return buf.Bytes(), writer.Close()
}

func newCRMMessageID(host string) string {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	domain := strings.TrimSpace(host)
	if domain == "" {
		domain = "multica.local"
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), base64.RawURLEncoding.EncodeToString(raw), domain)
}

func sanitizeCRMSendError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	for _, marker := range []string{"password=", "pass=", "secret=", "token=", "Authorization:"} {
		idx := strings.Index(strings.ToLower(msg), strings.ToLower(marker))
		if idx >= 0 {
			end := strings.IndexAny(msg[idx+len(marker):], " \t\r\n&")
			if end < 0 {
				msg = msg[:idx+len(marker)] + "[redacted]"
			} else {
				end += idx + len(marker)
				msg = msg[:idx+len(marker)] + "[redacted]" + msg[end:]
			}
		}
	}
	return fmt.Errorf("%s", msg)
}
