package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// CloudFrontSigner generates signed cookies for CloudFront private distributions.
type CloudFrontSigner struct {
	keyPairID    string
	privateKey   *rsa.PrivateKey
	domain       string // CDN domain, e.g. "static.multica.ai"
	cookieDomain string // cookie scope, e.g. ".multica.ai"
}

// NewCloudFrontSignerFromEnv creates a signer from environment variables.
// Returns nil if CLOUDFRONT_KEY_PAIR_ID is not set (disables signed cookies).
//
// Private key resolution order:
//  1. AWS Secrets Manager (CLOUDFRONT_PRIVATE_KEY_SECRET — secret name/ARN)
//  2. Environment variable fallback (CLOUDFRONT_PRIVATE_KEY — base64-encoded PEM, for local dev only)
//
// Other required environment variables:
//   - CLOUDFRONT_KEY_PAIR_ID
//   - CLOUDFRONT_DOMAIN       (e.g. "static.multica.ai")
//   - COOKIE_DOMAIN           (e.g. ".multica.ai")
func NewCloudFrontSignerFromEnv() *CloudFrontSigner {
	keyPairID := os.Getenv("CLOUDFRONT_KEY_PAIR_ID")
	if keyPairID == "" {
		slog.Info("CLOUDFRONT_KEY_PAIR_ID not set, signed cookies disabled")
		return nil
	}

	domain := os.Getenv("CLOUDFRONT_DOMAIN")
	if domain == "" {
		slog.Error("CLOUDFRONT_DOMAIN not set")
		return nil
	}

	cookieDomain := os.Getenv("COOKIE_DOMAIN")
	if cookieDomain == "" {
		slog.Error("COOKIE_DOMAIN not set")
		return nil
	}

	rsaKey, err := loadPrivateKey()
	if err != nil {
		slog.Error("failed to load CloudFront private key", "error", err)
		return nil
	}

	slog.Info("CloudFront cookie signer initialized", "key_pair_id", keyPairID, "domain", domain)
	return &CloudFrontSigner{
		keyPairID:    keyPairID,
		privateKey:   rsaKey,
		domain:       domain,
		cookieDomain: cookieDomain,
	}
}

// loadPrivateKey loads the RSA private key from Secrets Manager or env var fallback.
func loadPrivateKey() (*rsa.PrivateKey, error) {
	// 1. Try Secrets Manager
	if secretName := os.Getenv("CLOUDFRONT_PRIVATE_KEY_SECRET"); secretName != "" {
		slog.Info("loading CloudFront private key from Secrets Manager", "secret", secretName)
		return loadKeyFromSecretsManager(secretName)
	}

	// 2. Fallback: base64-encoded env var (local dev)
	if pkB64 := os.Getenv("CLOUDFRONT_PRIVATE_KEY"); pkB64 != "" {
		slog.Info("loading CloudFront private key from environment variable (local dev)")
		pemBytes, err := base64.StdEncoding.DecodeString(pkB64)
		if err != nil {
			return nil, fmt.Errorf("base64 decode: %w", err)
		}
		return parseRSAPrivateKey(pemBytes)
	}

	return nil, fmt.Errorf("neither CLOUDFRONT_PRIVATE_KEY_SECRET nor CLOUDFRONT_PRIVATE_KEY is set")
}

func loadKeyFromSecretsManager(secretName string) (*rsa.PrivateKey, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := secretsmanager.NewFromConfig(cfg)
	result, err := client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	})
	if err != nil {
		return nil, fmt.Errorf("get secret %q: %w", secretName, err)
	}

	if result.SecretString == nil {
		return nil, fmt.Errorf("secret %q has no string value", secretName)
	}

	return parseRSAPrivateKey([]byte(*result.SecretString))
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	// Try PKCS8 first, then PKCS1
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("PKCS8 key is not RSA")
	}

	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return rsaKey, nil
}

// SignedCookies generates the three CloudFront signed cookies with the given expiry.
func (s *CloudFrontSigner) SignedCookies(expiry time.Time) []*http.Cookie {
	policy := fmt.Sprintf(`{"Statement":[{"Resource":"https://%s/*","Condition":{"DateLessThan":{"AWS:EpochTime":%d}}}]}`, s.domain, expiry.Unix())

	encodedPolicy := cfBase64Encode([]byte(policy))

	h := sha1.New()
	h.Write([]byte(policy))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA1, h.Sum(nil))
	if err != nil {
		slog.Error("failed to sign CloudFront policy", "error", err)
		return nil
	}
	encodedSig := cfBase64Encode(sig)

	cookieAttrs := func(name, value string) *http.Cookie {
		return &http.Cookie{
			Name:     name,
			Value:    value,
			Domain:   s.cookieDomain,
			Path:     "/",
			Expires:  expiry,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteNoneMode,
		}
	}

	return []*http.Cookie{
		cookieAttrs("CloudFront-Policy", encodedPolicy),
		cookieAttrs("CloudFront-Signature", encodedSig),
		cookieAttrs("CloudFront-Key-Pair-Id", s.keyPairID),
	}
}

// SignedURL generates a CloudFront signed URL for the given resource URL.
// Used by CLI/API clients that don't have browser cookies.
func (s *CloudFrontSigner) SignedURL(rawURL string, expiry time.Time) string {
	policy := fmt.Sprintf(`{"Statement":[{"Resource":"%s","Condition":{"DateLessThan":{"AWS:EpochTime":%d}}}]}`, rawURL, expiry.Unix())

	encodedPolicy := cfBase64Encode([]byte(policy))

	h := sha1.New()
	h.Write([]byte(policy))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA1, h.Sum(nil))
	if err != nil {
		slog.Error("failed to sign CloudFront URL", "error", err)
		return rawURL
	}
	encodedSig := cfBase64Encode(sig)

	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%sPolicy=%s&Signature=%s&Key-Pair-Id=%s", rawURL, separator, encodedPolicy, encodedSig, s.keyPairID)
}

// cfBase64Encode applies CloudFront's URL-safe base64 encoding.
func cfBase64Encode(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	r := strings.NewReplacer("+", "-", "=", "_", "/", "~")
	return r.Replace(encoded)
}
