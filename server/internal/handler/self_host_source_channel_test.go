package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/sourcechannel"
)

func TestRecordSelfHostSourceChannelUpsertsByAnonymousSubject(t *testing.T) {
	instanceHash := strings.Repeat("a", 64)
	subjectHash := strings.Repeat("b", 64)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM self_host_source_channel WHERE instance_hash = $1 AND subject_hash = $2`,
			instanceHash,
			subjectHash,
		)
	})

	postSourceReport(t, sourcechannel.Report{
		SchemaVersion: sourcechannel.SchemaVersion,
		Channel:       "other",
		InstanceHash:  instanceHash,
		SubjectHash:   subjectHash,
		SourceOther:   "  a podcast  ",
		Domain:        stringPtr("example.com"),
		DomainMD5:     sourcechannel.DomainMD5("example.com"),
	})
	postSourceReport(t, sourcechannel.Report{
		SchemaVersion: sourcechannel.SchemaVersion,
		Channel:       "search",
		InstanceHash:  instanceHash,
		SubjectHash:   subjectHash,
		Domain:        stringPtr("example.com"),
		DomainMD5:     sourcechannel.DomainMD5("example.com"),
	})

	var (
		channel     string
		sourceOther pgtype.Text
		domain      pgtype.Text
		domainMD5   pgtype.Text
		reportCount int
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT channel, source_other, domain, domain_md5, report_count
		  FROM self_host_source_channel
		 WHERE instance_hash = $1 AND subject_hash = $2
	`, instanceHash, subjectHash).Scan(&channel, &sourceOther, &domain, &domainMD5, &reportCount); err != nil {
		t.Fatalf("load source channel row: %v", err)
	}
	if channel != "search" {
		t.Fatalf("channel: want latest value search, got %q", channel)
	}
	if sourceOther.Valid {
		t.Fatalf("source_other should clear when latest channel is not other, got %q", sourceOther.String)
	}
	if !domain.Valid || domain.String != "example.com" {
		t.Fatalf("domain: want example.com, got %+v", domain)
	}
	if !domainMD5.Valid || domainMD5.String != sourcechannel.DomainMD5("example.com") {
		t.Fatalf("domain_md5: want %q, got %+v", sourcechannel.DomainMD5("example.com"), domainMD5)
	}
	if reportCount != 2 {
		t.Fatalf("report_count: want 2, got %d", reportCount)
	}
}

func TestRecordSelfHostSourceChannelStoresOtherText(t *testing.T) {
	instanceHash := strings.Repeat("c", 64)
	subjectHash := strings.Repeat("d", 64)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM self_host_source_channel WHERE instance_hash = $1 AND subject_hash = $2`,
			instanceHash,
			subjectHash,
		)
	})

	postSourceReport(t, sourcechannel.Report{
		SchemaVersion: sourcechannel.SchemaVersion,
		Channel:       "other",
		InstanceHash:  instanceHash,
		SubjectHash:   subjectHash,
		SourceOther:   "  private free text  ",
		Domain:        stringPtr("example.com"),
		DomainMD5:     sourcechannel.DomainMD5("example.com"),
	})

	var sourceOther pgtype.Text
	if err := testPool.QueryRow(context.Background(), `
		SELECT source_other
		  FROM self_host_source_channel
		 WHERE instance_hash = $1 AND subject_hash = $2
	`, instanceHash, subjectHash).Scan(&sourceOther); err != nil {
		t.Fatalf("load source_other: %v", err)
	}
	if !sourceOther.Valid || sourceOther.String != "private free text" {
		t.Fatalf("source_other: want trimmed text, got %+v", sourceOther)
	}
}

func TestRecordSelfHostSourceChannelRejectsInvalidDomainHash(t *testing.T) {
	payload := sourcechannel.Report{
		SchemaVersion: sourcechannel.SchemaVersion,
		Channel:       "search",
		InstanceHash:  strings.Repeat("e", 64),
		SubjectHash:   strings.Repeat("f", 64),
		Domain:        stringPtr("example.com"),
		DomainMD5:     sourcechannel.DomainMD5("other.example"),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/acquisition/self-host-source", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	testHandler.RecordSelfHostSourceChannel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid domain_md5, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRecordSelfHostSourceChannelAcceptsHashOnlyDomain(t *testing.T) {
	instanceHash := strings.Repeat("1", 64)
	subjectHash := strings.Repeat("2", 64)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM self_host_source_channel WHERE instance_hash = $1 AND subject_hash = $2`,
			instanceHash,
			subjectHash,
		)
	})

	postSourceReport(t, sourcechannel.Report{
		SchemaVersion: sourcechannel.SchemaVersion,
		Channel:       "search",
		InstanceHash:  instanceHash,
		SubjectHash:   subjectHash,
		Domain:        nil,
		DomainMD5:     sourcechannel.DomainMD5("example.com"),
	})

	var (
		domain    pgtype.Text
		domainMD5 pgtype.Text
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT domain, domain_md5
		  FROM self_host_source_channel
		 WHERE instance_hash = $1 AND subject_hash = $2
	`, instanceHash, subjectHash).Scan(&domain, &domainMD5); err != nil {
		t.Fatalf("load source channel row: %v", err)
	}
	if domain.Valid {
		t.Fatalf("domain should be NULL when plaintext domain is omitted, got %+v", domain)
	}
	if !domainMD5.Valid || domainMD5.String != sourcechannel.DomainMD5("example.com") {
		t.Fatalf("domain_md5: want %q, got %+v", sourcechannel.DomainMD5("example.com"), domainMD5)
	}
}

func postSourceReport(t *testing.T, payload sourcechannel.Report) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/acquisition/self-host-source", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	testHandler.RecordSelfHostSourceChannel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RecordSelfHostSourceChannel: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func stringPtr(s string) *string {
	return &s
}
