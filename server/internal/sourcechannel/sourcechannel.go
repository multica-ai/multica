package sourcechannel

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

const SchemaVersion = 1
const SourceOtherMaxRunes = 512
const DomainMaxLength = 255
const OfficialMulticaAPIURL = "https://api.multica.ai"
const officialMulticaAPIHost = "api.multica.ai"

type Report struct {
	SchemaVersion int     `json:"schema_version"`
	Channel       string  `json:"channel"`
	InstanceHash  string  `json:"instance_hash"`
	SubjectHash   string  `json:"subject_hash"`
	SourceOther   string  `json:"source_other,omitempty"`
	Domain        *string `json:"domain"`
	DomainMD5     string  `json:"domain_md5"`
}

var validChannels = map[string]struct{}{
	"friends_colleagues": {},
	"search":             {},
	"social_x":           {},
	"social_linkedin":    {},
	"social_youtube":     {},
	"social_github":      {},
	"social_other":       {},
	"blog_newsletter":    {},
	"ai_assistant":       {},
	"from_work":          {},
	"event_conference":   {},
	"dont_remember":      {},
	"other":              {},
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var md5Pattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func NormalizeChannel(channel string) string {
	return strings.ToLower(strings.TrimSpace(channel))
}

func ValidChannel(channel string) bool {
	_, ok := validChannels[NormalizeChannel(channel)]
	return ok
}

func NormalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func ValidHash(hash string) bool {
	return hashPattern.MatchString(NormalizeHash(hash))
}

func ValidDomainMD5(hash string) bool {
	return md5Pattern.MatchString(NormalizeHash(hash))
}

func NormalizeSourceOther(channel, sourceOther string) string {
	if NormalizeChannel(channel) != "other" {
		return ""
	}
	sourceOther = strings.TrimSpace(sourceOther)
	if sourceOther == "" {
		return ""
	}
	runes := []rune(sourceOther)
	if len(runes) > SourceOtherMaxRunes {
		sourceOther = string(runes[:SourceOtherMaxRunes])
	}
	return sourceOther
}

func ReportingAPIBaseURL(r *http.Request) string {
	if base := strings.TrimSpace(os.Getenv("MULTICA_PUBLIC_URL")); base != "" {
		return base
	}
	if r == nil {
		return ""
	}
	return r.Host
}

func ShouldReportAPIBaseURL(apiBaseURL string) bool {
	domain := NormalizeDomain(apiBaseURL)
	return domain != "" && !IsOfficialMulticaAPIURL(apiBaseURL)
}

func NormalizeDomain(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			raw = u.Host
		}
	} else if u, err := url.Parse("//" + raw); err == nil {
		raw = u.Host
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	} else if strings.Count(raw, ":") == 1 {
		if host, _, err := net.SplitHostPort(raw); err == nil {
			raw = host
		} else if i := strings.LastIndex(raw, ":"); i > 0 {
			raw = raw[:i]
		}
	}
	raw = strings.Trim(raw, "[]")
	raw = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if raw == "" || len(raw) > DomainMaxLength {
		return ""
	}
	return raw
}

func IsOfficialMulticaAPIURL(apiBaseURL string) bool {
	raw := strings.TrimSpace(apiBaseURL)
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return false
		}
		path := strings.TrimRight(u.EscapedPath(), "/")
		return u.Scheme == "https" &&
			strings.EqualFold(u.Hostname(), officialMulticaAPIHost) &&
			(u.Port() == "" || u.Port() == "443") &&
			path == ""
	}
	return NormalizeDomain(raw) == officialMulticaAPIHost
}

func DomainMD5(domain string) string {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return ""
	}
	sum := md5.Sum([]byte(domain))
	return hex.EncodeToString(sum[:])
}

func InstanceHash(salt string) string {
	return hashHex(salt, "instance", "multica")
}

func SubjectHash(salt, userID string) string {
	return hashHex(salt, "subject", userID)
}

func hashHex(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
