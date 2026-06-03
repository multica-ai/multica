package logger

import (
	"os"
	"strings"
)

// RequestLogMode controls how much request input data is logged.
type RequestLogMode string

const (
	RequestLogSummary  RequestLogMode = "summary"
	RequestLogEnhanced RequestLogMode = "enhanced"
	RequestLogFull     RequestLogMode = "full"
)

// Config contains runtime logging controls read from environment variables.
type Config struct {
	RequestMode    RequestLogMode
	SQLDetail      bool
	ResponseDetail bool
}

// ConfigFromEnv reads logging controls from environment variables.
func ConfigFromEnv() Config {
	return Config{
		RequestMode:    ParseRequestLogMode(os.Getenv("LOG_REQUEST_MODE")),
		SQLDetail:      parseBool(os.Getenv("LOG_SQL_DETAIL")),
		ResponseDetail: parseBool(os.Getenv("LOG_RESPONSE_DETAIL")),
	}
}

// ParseRequestLogMode returns a safe request logging mode for user input.
func ParseRequestLogMode(raw string) RequestLogMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(RequestLogEnhanced):
		return RequestLogEnhanced
	case string(RequestLogFull):
		return RequestLogFull
	default:
		return RequestLogSummary
	}
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}
