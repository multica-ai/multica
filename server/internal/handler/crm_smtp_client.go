package handler

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

func sendCRMSMTP(cfg crmIMAPMailboxConfig, to, cc, bcc []string, subject, body string) error {
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		return fmt.Errorf("SMTP host is required")
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
		return err
	}
	from := cfg.Email
	recipients := append(append(append([]string{}, to...), cc...), bcc...)
	if len(recipients) == 0 {
		return fmt.Errorf("recipient is required")
	}
	addr := net.JoinHostPort(cfg.SMTPHost, fmt.Sprintf("%d", port))
	message := buildCRMSMTPMessage(from, to, cc, subject, body)
	auth := smtp.PlainAuth("", username, password, cfg.SMTPHost)
	mode := strings.ToLower(strings.TrimSpace(cfg.SMTPTLSMode))
	if mode == "" || mode == "ssl" {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12})
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, cfg.SMTPHost)
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
		if _, err := wc.Write([]byte(message)); err != nil {
			_ = wc.Close()
			return err
		}
		return wc.Close()
	}
	return smtp.SendMail(addr, auth, from, recipients, []byte(message))
}

func buildCRMSMTPMessage(from string, to, cc []string, subject, body string) string {
	lines := []string{
		"From: " + from,
		"To: " + strings.Join(to, ", "),
		"Subject: " + strings.ReplaceAll(subject, "\n", " "),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	if len(cc) > 0 {
		lines = append(lines[:2], append([]string{"Cc: " + strings.Join(cc, ", ")}, lines[2:]...)...)
	}
	lines = append(lines, "", body)
	return strings.Join(lines, "\r\n")
}
