package auth

// MULTICA-LOCAL: CloudFront signing removed.
// This stub satisfies references in handlers where CFSigner is nil-checked.

import (
	"net/http"
	"time"
)

type CloudFrontSigner struct{}

func NewCloudFrontSignerFromEnv() *CloudFrontSigner {
	return nil // No CloudFront in local mode
}

func (s *CloudFrontSigner) SignedCookies(expires time.Time) []*http.Cookie {
	return nil
}

func (s *CloudFrontSigner) SignedURL(rawURL string, expires time.Time) string {
	return rawURL
}
