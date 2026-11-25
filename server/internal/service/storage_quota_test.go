package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
)

type fakeStorageSums struct {
	used  int64
	err   error
	block bool
	calls int
}

func (f *fakeStorageSums) SumWorkspaceAttachmentBytes(ctx context.Context, _ pgtype.UUID) (int64, error) {
	f.calls++
	if f.block {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return f.used, f.err
}

type fakeStorageMetrics struct {
	mu        sync.Mutex
	sums      []string
	decisions []string
}

func (m *fakeStorageMetrics) RecordStorageQuotaSum(source, result string, _ float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sums = append(m.sums, source+":"+result)
}

func (m *fakeStorageMetrics) RecordStorageQuotaDecision(action, source, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions = append(m.decisions, action+":"+source+":"+result)
}

func storageDecision(action entitlement.Action, limit int) entitlement.Decision {
	gate := entitlement.Gate{Action: action}
	if action != entitlement.ActionOff {
		gate.Limit = &limit
	}
	return entitlement.Decision{Gate: gate}
}

func newTestStorageQuota(used int64, action entitlement.Action, limit int) (StorageQuota, *fakeStorageSums, *entitlementtest.Stub, pgtype.UUID) {
	id := uuid.New()
	stub := entitlementtest.New()
	stub.Set(id, entitlement.GateStorageBytes, storageDecision(action, limit))
	sums := &fakeStorageSums{used: used}
	return StorageQuota{Sums: sums, Entitlements: stub, Metrics: &fakeStorageMetrics{}}, sums, stub, pgtype.UUID{Bytes: id, Valid: true}
}

func TestStorageQuotaOffReadsNothing(t *testing.T) {
	q, sums, _, ws := newTestStorageQuota(5000, entitlement.ActionOff, 0)
	if err := q.Admit(context.Background(), ws, 100, StorageSourceUpload); err != nil {
		t.Fatalf("off admit = %v", err)
	}
	q.RecordStored(context.Background(), ws, StorageSourceChannel)
	if usage := q.Usage(context.Background(), ws); usage.Action != entitlement.ActionOff || usage.Used != nil {
		t.Fatalf("off usage = %+v", usage)
	}
	if sums.calls != 0 {
		t.Fatalf("off summed %d times, want none", sums.calls)
	}
}

// Self-hosted: no provider at all.
func TestStorageQuotaWithoutProviderAdmitsEverything(t *testing.T) {
	sums := &fakeStorageSums{used: 1 << 40}
	q := StorageQuota{Sums: sums}
	ws := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if err := q.Admit(context.Background(), ws, 1<<30, StorageSourceUpload); err != nil || sums.calls != 0 {
		t.Fatalf("admit = %v, sums = %d", err, sums.calls)
	}
}

func TestStorageQuotaActions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		action  entitlement.Action
		bytes   int64
		wantErr bool
	}{
		{"observe over", entitlement.ActionObserve, 200, false},
		{"enforce within", entitlement.ActionEnforce, 100, false},
		{"enforce over", entitlement.ActionEnforce, 101, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _, _, ws := newTestStorageQuota(900, tc.action, 1000)
			err := q.Admit(context.Background(), ws, tc.bytes, StorageSourceUpload)
			var reached *StorageLimitReachedError
			if gotErr := errors.As(err, &reached); gotErr != tc.wantErr {
				t.Fatalf("admit = %v, want refused %v", err, tc.wantErr)
			}
			if reached != nil && (reached.Used != 900 || reached.Limit != 1000) {
				t.Fatalf("refusal = %+v", reached)
			}
		})
	}
}

func TestStorageQuotaRefreshesBeforeRefusing(t *testing.T) {
	q, _, stub, ws := newTestStorageQuota(900, entitlement.ActionEnforce, 1000)
	stub.SetRefreshed(uuid.UUID(ws.Bytes), entitlement.GateStorageBytes, storageDecision(entitlement.ActionEnforce, 5000))
	if err := q.Admit(context.Background(), ws, 500, StorageSourceUpload); err != nil {
		t.Fatalf("admit after upgrade = %v", err)
	}
	if len(stub.Refreshes()) != 1 {
		t.Fatalf("refreshes = %d, want 1", len(stub.Refreshes()))
	}
	// Within the limit nothing is refreshed.
	if err := q.Admit(context.Background(), ws, 1, StorageSourceUpload); err != nil || len(stub.Refreshes()) != 1 {
		t.Fatalf("admit = %v, refreshes = %d", err, len(stub.Refreshes()))
	}
}

// A sum that runs past its timeout admits the upload and is counted, so a
// slow query never holds uploads.
func TestStorageQuotaSumTimeoutFailsOpen(t *testing.T) {
	q, sums, _, ws := newTestStorageQuota(0, entitlement.ActionEnforce, 1)
	sums.block = true
	q.SumTimeout = 20 * time.Millisecond
	metrics := q.Metrics.(*fakeStorageMetrics)

	started := time.Now()
	if err := q.Admit(context.Background(), ws, 1<<20, StorageSourceUpload); err != nil {
		t.Fatalf("timed-out admit = %v, want admitted", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("admit took %s, want about the timeout", elapsed)
	}
	if len(metrics.sums) != 1 || metrics.sums[0] != "upload:timeout" ||
		len(metrics.decisions) != 1 || metrics.decisions[0] != "enforce:upload:fail_open" {
		t.Fatalf("metrics = %+v / %+v", metrics.sums, metrics.decisions)
	}
}

func TestStorageQuotaSumErrorFailsOpen(t *testing.T) {
	q, sums, _, ws := newTestStorageQuota(0, entitlement.ActionEnforce, 1)
	sums.err = errors.New("connection reset")
	if err := q.Admit(context.Background(), ws, 1<<20, StorageSourceUpload); err != nil {
		t.Fatalf("errored admit = %v, want admitted", err)
	}
	if got := q.Metrics.(*fakeStorageMetrics).sums; len(got) != 1 || got[0] != "upload:error" {
		t.Fatalf("sum metrics = %+v", got)
	}
	// The billing read keeps the limit and leaves usage unknown.
	if usage := q.Usage(context.Background(), ws); usage.Action != entitlement.ActionEnforce ||
		usage.Limit == nil || *usage.Limit != 1 || usage.Used != nil || usage.Reached != nil {
		t.Fatalf("usage after a failed sum = %+v, want the limit without figures", usage)
	}
}

// Channel files are stored before anything is checked and never refused; a
// workspace they put over its limit is recorded as admitted.
func TestStorageQuotaRecordStoredNeverRefuses(t *testing.T) {
	q, sums, _, ws := newTestStorageQuota(1500, entitlement.ActionEnforce, 1000)
	q.RecordStored(context.Background(), ws, StorageSourceChannel)
	if sums.calls != 1 {
		t.Fatalf("sums = %d, want 1", sums.calls)
	}
	if got := q.Metrics.(*fakeStorageMetrics).decisions; len(got) != 1 || got[0] != "observe:channel:observed" {
		t.Fatalf("decisions = %+v", got)
	}
}

func TestStorageLimitPeriodIsTheUTCMonth(t *testing.T) {
	start, end := StorageLimitPeriod(time.Date(2026, 11, 30, 23, 0, 0, 0, time.FixedZone("x", -3*3600)))
	if !start.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("period = %s..%s", start, end)
	}
}

// A sum that runs past its timeout on the billing read reports the limit with
// usage unknown, rather than failing the whole read.
func TestStorageQuotaUsageTimeoutKeepsTheLimit(t *testing.T) {
	q, sums, _, ws := newTestStorageQuota(0, entitlement.ActionObserve, 1000)
	sums.block = true
	q.SumTimeout = 20 * time.Millisecond
	usage := q.Usage(context.Background(), ws)
	if usage.Action != entitlement.ActionObserve || usage.Limit == nil || *usage.Limit != 1000 || usage.Used != nil {
		t.Fatalf("usage = %+v, want observe 1000 with usage unknown", usage)
	}
}
