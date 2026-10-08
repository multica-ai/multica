package telegram

import (
	"context"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestRandomBindingTokenIsUniqueAndURLSafe(t *testing.T) {
	first, err := randomBindingToken(32)
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	second, err := randomBindingToken(32)
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if first == second {
		t.Fatal("two random binding tokens were identical")
	}
	if ok := regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(first); !ok {
		t.Fatalf("token is not raw URL-safe base64: %q", first)
	}
}

func TestHashBindingTokenIsDeterministicAndDoesNotRetainRawValue(t *testing.T) {
	const raw = "binding-token-sentinel"
	first := hashBindingToken(raw)
	second := hashBindingToken(raw)
	if first != second {
		t.Fatalf("hash changed between calls: %q != %q", first, second)
	}
	if first == raw || len(first) != 64 {
		t.Fatalf("unexpected SHA-256 representation: %q", first)
	}
}

func TestRedeemAndBindRequiresTransactionStarter(t *testing.T) {
	service := &BindingTokenService{}
	_, err := service.RedeemAndBind(context.Background(), "token", pgtype.UUID{})
	if err == nil || err.Error() != "telegram: BindingTokenService missing TxStarter" {
		t.Fatalf("error = %v", err)
	}
}
