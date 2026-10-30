package telegram

import (
	"errors"
	"net/http"
	"testing"
)

func TestClassifyCredentialVerificationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "Telegram rejects the token",
			err:  &apiError{Code: http.StatusUnauthorized, Description: "Unauthorized"},
			want: ErrCredentialsRejected,
		},
		{
			name: "Telegram rate limits verification",
			err:  &apiError{Code: http.StatusTooManyRequests, Description: "Too Many Requests"},
			want: ErrCredentialsUnverifiable,
		},
		{
			name: "transport cannot reach Telegram",
			err:  &requestError{method: "getMe", cause: errors.New("proxy connect failed")},
			want: ErrCredentialsUnverifiable,
		},
		{
			name: "malformed upstream response",
			err:  errors.New("decode response"),
			want: ErrCredentialsUnverifiable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := classifyCredentialVerificationError(tt.err); !errors.Is(err, tt.want) {
				t.Fatalf("classifyCredentialVerificationError() = %v, want %v", err, tt.want)
			}
		})
	}
}
