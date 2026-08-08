package analytics

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
)

const acquisitionDimensionMaxLength = 96

type acquisitionAttribution struct {
	Source       string `json:"source,omitempty"`
	Medium       string `json:"medium,omitempty"`
	Campaign     string `json:"campaign,omitempty"`
	ReferrerHost string `json:"referrer_host,omitempty"`
}

// ParseAcquisitionAttribution reduces the first-touch cookie to the bounded,
// non-content dimensions that reporting needs. Invalid or empty input produces
// nil so account creation remains independent of analytics.
func ParseAcquisitionAttribution(raw string) []byte {
	var source map[string]any
	if raw == "" || json.Unmarshal([]byte(raw), &source) != nil {
		return nil
	}

	attribution := acquisitionAttribution{
		Source:   sanitizeAcquisitionDimension(stringValue(source, "utm_source")),
		Medium:   sanitizeAcquisitionDimension(stringValue(source, "utm_medium")),
		Campaign: sanitizeAcquisitionDimension(stringValue(source, "utm_campaign")),
	}
	if origin := stringValue(source, "referrer_origin"); origin != "" {
		if parsed, err := url.Parse(origin); err == nil {
			attribution.ReferrerHost = sanitizeAcquisitionDimension(parsed.Hostname())
		}
	}
	if attribution.Source == "" {
		attribution.Source = attribution.ReferrerHost
		if attribution.Source == "" {
			attribution.Source = "direct"
		}
	}
	if attribution.Medium == "" {
		attribution.Medium = "none"
	}
	if attribution.Campaign == "" {
		attribution.Campaign = "none"
	}

	encoded, err := json.Marshal(attribution)
	if err != nil {
		return nil
	}
	return encoded
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func sanitizeAcquisitionDimension(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return ""
	}
	if strings.Contains(value, "@") || strings.Contains(value, "://") || strings.ContainsAny(value, "/?#") {
		return ""
	}

	var sanitized strings.Builder
	for _, r := range value {
		if sanitized.Len() >= acquisitionDimensionMaxLength {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			sanitized.WriteRune(r)
		case r == '.', r == '_', r == '-':
			sanitized.WriteRune(r)
		case r == ' ':
			sanitized.WriteByte('_')
		}
	}
	return strings.ToLower(sanitized.String())
}
