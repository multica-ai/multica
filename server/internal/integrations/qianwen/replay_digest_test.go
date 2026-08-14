package qianwen

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPairingReplayDigestUsesTimestampNonceTuple(t *testing.T) {
	service := &Service{pairingDigestKey: []byte("deterministic-qianwen-pairing-key")}
	installationID := pgtype.UUID{
		Bytes: uuid.MustParse("2e76f9d0-c552-49b0-9c26-aed995c95ba5"),
		Valid: true,
	}
	nonce := "0123456789abcdef0123456789abcdef"

	first := service.pairingNonceDigest(installationID, "1786723200000", nonce)
	sameTuple := service.pairingNonceDigest(installationID, "1786723200000", nonce)
	if !bytes.Equal(first, sameTuple) {
		t.Fatal("the same timestamp and nonce must produce the same replay digest")
	}

	differentTimestamp := service.pairingNonceDigest(installationID, "1786723200001", nonce)
	if bytes.Equal(first, differentTimestamp) {
		t.Fatal("the same nonce with a different timestamp must produce a different replay digest")
	}
}
