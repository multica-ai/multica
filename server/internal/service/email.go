package service

// MULTICA-LOCAL: Email service removed. This is a no-op stub.

import "fmt"

type EmailService struct{}

func NewEmailService() *EmailService {
	return &EmailService{}
}

func (s *EmailService) SendVerificationCode(to, code string) error {
	fmt.Printf("[LOCAL] Verification code for %s: %s\n", to, code)
	return nil
}
