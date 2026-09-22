package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/llm"
)

const (
	knowledgeJobLease                = 120 * time.Second
	knowledgeJobPoll                 = 750 * time.Millisecond
	knowledgeProviderRetryAfterLimit = 15 * time.Minute
	knowledgeWorkerConcurrency       = 2
)

type workerJob struct {
	Job
	WorkspaceID string
	Input       map[string]any
	LeaseToken  pgtype.UUID
}

func scanWorkerJob(row pgx.Row, token pgtype.UUID) (workerJob, error) {
	var job workerJob
	var versionID, indexID, errorCode pgtype.Text
	var input, progress []byte
	err := row.Scan(&job.ID, &job.WorkspaceID, &job.KnowledgeBaseID, &versionID, &indexID, &job.Stage, &job.Status, &job.Attempt, &job.AvailableAt, &input, &progress, &errorCode, &job.CreatedAt)
	if err != nil {
		return workerJob{}, err
	}
	job.DocumentVersionID = nullableText(versionID)
	job.IndexID = nullableText(indexID)
	job.Input = mapJSON(input)
	job.Progress = mapJSON(progress)
	job.ErrorCode = nullableText(errorCode)
	job.LeaseToken = token
	return job, nil
}

func (s *Service) claimJob(ctx context.Context) (*workerJob, error) {
	token, _ := newID()
	job, err := scanWorkerJob(s.db.QueryRow(ctx, `
		WITH workspace_candidates AS (
			SELECT DISTINCT ON (workspace_id) id,created_at
			FROM knowledge_job
			WHERE status='queued' AND available_at<=now()
			ORDER BY workspace_id,created_at,id
		),
		candidate AS (
			SELECT j.id
			FROM knowledge_job j JOIN workspace_candidates c ON c.id=j.id
			WHERE j.status='queued' AND j.available_at<=now()
			ORDER BY j.created_at,j.id
			FOR UPDATE OF j SKIP LOCKED LIMIT 1
		)
		UPDATE knowledge_job j SET status='running',attempt=j.attempt+1,lease_token=$1,
			lease_until=now()+$2::interval,heartbeat_at=now(),updated_at=now()
		FROM candidate c WHERE j.id=c.id
		RETURNING j.id::text,j.workspace_id::text,j.knowledge_base_id::text,j.document_version_id::text,j.index_id::text,
		          j.stage,j.status,j.attempt,j.available_at,j.input,j.progress,j.error_code,j.created_at`, token, knowledgeJobLease.String()), token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Service) heartbeatJob(ctx context.Context, job *workerJob) {
	if job == nil {
		return
	}
	_, _ = s.db.Exec(ctx, `UPDATE knowledge_job SET lease_until=now()+$3::interval,heartbeat_at=now(),updated_at=now() WHERE id=$1 AND lease_token=$2 AND status='running'`, mustID(job.ID), job.LeaseToken, knowledgeJobLease.String())
}

// fenceKnowledgeJob is the last-write guard for every worker stage. A lease
// token alone is not sufficient: after the lease deadline a stale worker must
// stop even if the recovery loop has not yet replaced its token. Callers that
// are about to write use lock=true so the status check and the following
// writes share one row lock.
func fenceKnowledgeJob(ctx context.Context, q DBTX, job *workerJob, lock bool) error {
	if job == nil {
		return badRequest("invalid_job", "knowledge job is missing")
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var status string
	err := q.QueryRow(ctx, `SELECT status FROM knowledge_job WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()`+suffix, mustID(job.ID), job.LeaseToken).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("job_cancelled", "the knowledge job lease is no longer valid")
	}
	if err != nil {
		return internal("failed to fence knowledge job", err)
	}
	if status != JobRunning {
		return conflict("job_cancelled", "the knowledge job is no longer running")
	}
	return nil
}

func (s *Service) finishJob(ctx context.Context, job *workerJob, result any) error {
	data, err := jsonBytes(result)
	if err != nil {
		data = []byte("{}")
	}
	_, err = s.db.Exec(ctx, `UPDATE knowledge_job SET status='succeeded',lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,result_ref=$3,updated_at=now() WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()`, mustID(job.ID), job.LeaseToken, data)
	return err
}

func (s *Service) waitJob(ctx context.Context, job *workerJob, reason string) error {
	progress, _ := jsonBytes(map[string]any{"waiting_reason": reason})
	_, err := s.db.Exec(ctx, `UPDATE knowledge_job SET status='waiting_config',lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,progress=$3,updated_at=now() WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()`, mustID(job.ID), job.LeaseToken, progress)
	return err
}

// resumeWaitingConfigJobs is called inside the same short transaction as a
// configuration mutation. Waiting jobs are not polled by claimJob: without
// this explicit wake-up, adding a provider or fixing a model binding would
// leave the job parked until a user manually reprocessed every document.
func resumeWaitingConfigJobs(ctx context.Context, q DBTX, workspace pgtype.UUID, baseID *pgtype.UUID) error {
	if baseID == nil {
		_, err := q.Exec(ctx, `UPDATE knowledge_job SET status='queued',available_at=now(),error_code=NULL,updated_at=now() WHERE workspace_id=$1 AND status='waiting_config'`, workspace)
		return err
	}
	_, err := q.Exec(ctx, `UPDATE knowledge_job SET status='queued',available_at=now(),error_code=NULL,updated_at=now() WHERE workspace_id=$1 AND knowledge_base_id=$2 AND status='waiting_config'`, workspace, *baseID)
	return err
}

func (s *Service) failJob(ctx context.Context, job *workerJob, code string, err error) error {
	message := code
	if err != nil {
		message = code
	}
	command, updateErr := s.db.Exec(ctx, `UPDATE knowledge_job SET status='failed',lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,error_code=$3,progress=jsonb_build_object('error',$3),updated_at=now() WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()`, mustID(job.ID), job.LeaseToken, message)
	if updateErr != nil || command.RowsAffected() != 1 {
		return updateErr
	}
	// A model/index failure must not make an otherwise readable parsed version
	// disappear. Source and parse failures own the document-version status;
	// embedding and extraction have independent retry/configuration state.
	if job.DocumentVersionID != nil && (job.Stage == "fetch" || job.Stage == "parse" || job.Stage == "chunk") {
		_, _ = s.db.Exec(ctx, `UPDATE knowledge_document_version SET status=CASE WHEN $2='unsupported_format' THEN 'unsupported' ELSE 'failed' END,error_code=$2 WHERE id=$1 AND status='processing'`, mustID(*job.DocumentVersionID), message)
	}
	return updateErr
}

func (s *Service) requeueJob(ctx context.Context, job *workerJob, delay time.Duration) error {
	_, err := s.db.Exec(ctx, `UPDATE knowledge_job SET status='queued',available_at=now()+$3::interval,lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,updated_at=now() WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()`, mustID(job.ID), job.LeaseToken, delay.String())
	return err
}

func (s *Service) recoverKnowledgeLeases(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, `UPDATE knowledge_job SET status='queued',lease_token=NULL,lease_until=NULL,heartbeat_at=NULL,available_at=now(),updated_at=now() WHERE status='running' AND lease_until<now()`); err != nil {
		return err
	}
	// Receipts are deliberately not part of a knowledge-base delete: they are
	// workspace-scoped and may still be needed to make a client retry safe. A
	// later sweep removes only completed receipts after their 24-hour replay
	// window; in-flight receipts remain available for the owning request.
	_, err := s.db.Exec(ctx, `DELETE FROM knowledge_request WHERE expires_at<now() AND status<>'processing'`)
	return err
}

// Run is the durable knowledge worker loop. It claims with SKIP LOCKED and a
// fencing token so multiple server replicas can process independent jobs
// without one replica completing a lease that another has reclaimed.
func (s *Service) Run(ctx context.Context) error {
	if err := s.checkEnabled(); err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	for index := 0; index < knowledgeWorkerConcurrency; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			s.runWorker(workerCtx)
		}()
	}
	recoveryDone := make(chan struct{})
	go func() {
		defer close(recoveryDone)
		recoverTicker := time.NewTicker(30 * time.Second)
		defer recoverTicker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-recoverTicker.C:
				_ = s.recoverKnowledgeLeases(workerCtx)
			}
		}
	}()
	workers.Wait()
	cancel()
	<-recoveryDone
	return ctx.Err()
}

func (s *Service) runWorker(ctx context.Context) {
	poll := time.NewTicker(knowledgeJobPoll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			job, err := s.claimJob(ctx)
			if err != nil || job == nil {
				continue
			}
			s.heartbeatJob(ctx, job)
			if err := s.processJobWithHeartbeat(ctx, job); err != nil {
				if shouldRetryKnowledgeJob(job, err) {
					_ = s.requeueJob(ctx, job, knowledgeRetryDelayForError(job, err))
					continue
				}
				code := "job_failed"
				var typed *Error
				if errors.As(err, &typed) && typed.Code != "" {
					code = typed.Code
				}
				_ = s.failJob(ctx, job, code, err)
			} else if job.Status == JobWaitingConfig {
				// A stage may set waiting_config itself when a capability is not
				// configured. Keep the lease fenced but do not mark it succeeded.
			} else {
				_ = s.finishJob(ctx, job, nil)
			}
		}
	}
}

// processJobWithHeartbeat keeps the lease alive while a parser or model
// provider is running. A single heartbeat before processJob is not enough for
// a large document, and lease reclamation during a long call would let a
// second worker race the first one.
func (s *Service) processJobWithHeartbeat(ctx context.Context, job *workerJob) error {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var heartbeatWG sync.WaitGroup
	heartbeatWG.Add(1)
	go func() {
		defer heartbeatWG.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				s.heartbeatJob(heartbeatCtx, job)
			}
		}
	}()
	release, err := s.acquireModelSlot(ctx, job)
	if err != nil {
		cancel()
		heartbeatWG.Wait()
		return err
	}
	err = s.processJob(ctx, job)
	release()
	cancel()
	heartbeatWG.Wait()
	return err
}

func (s *Service) acquireModelSlot(ctx context.Context, job *workerJob) (func(), error) {
	providerID := s.modelProviderKey(ctx, job)
	if providerID == "" {
		return func() {}, nil
	}
	s.modelGatesMu.Lock()
	gate := s.modelGates[providerID]
	if gate == nil {
		gate = make(chan struct{}, s.modelConcurrency)
		s.modelGates[providerID] = gate
	}
	s.modelGatesMu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// modelProviderKey is deliberately best effort. A configuration error must be
// handled by the stage itself (for example, by moving a job to waiting_config),
// not by the concurrency gate. Successful resolution gives every provider its
// own semaphore; the two worker goroutines can therefore process different
// providers concurrently without exceeding the configured per-provider cap.
func (s *Service) modelProviderKey(ctx context.Context, job *workerJob) string {
	if job == nil {
		return ""
	}
	var binding *ResolvedBinding
	var err error
	switch job.Stage {
	case "extract":
		binding, err = s.resolveBinding(ctx, job.WorkspaceID, job.KnowledgeBaseID, PurposeExtract)
	case "enhance":
		binding, err = s.resolveBinding(ctx, job.WorkspaceID, job.KnowledgeBaseID, PurposeParse)
	case "embed":
		if job.IndexID == nil {
			return ""
		}
		binding, _, err = s.resolveIndexEmbedding(ctx, job.WorkspaceID, job.KnowledgeBaseID, *job.IndexID)
	default:
		return ""
	}
	if err != nil || binding == nil {
		return ""
	}
	return binding.Provider.ID
}

func shouldRetryKnowledgeJob(job *workerJob, err error) bool {
	if job == nil {
		return false
	}
	// Cleanup is the last durable owner of private objects. It must keep
	// retrying while storage or the database is temporarily down; otherwise a
	// successful delete could permanently leak objects after the ordinary
	// three-attempt job budget is exhausted.
	if job.Stage != "cleanup" && job.Attempt >= 3 {
		return false
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Retryable {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var httpErr *llm.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.RetryAfterSet && httpErr.RetryAfter > knowledgeProviderRetryAfterLimit {
			return false
		}
		return httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
	}
	return false
}

func knowledgeRetryDelayForError(job *workerJob, err error) time.Duration {
	var httpErr *llm.HTTPError
	if errors.As(err, &httpErr) && httpErr.RetryAfterSet && httpErr.RetryAfter <= knowledgeProviderRetryAfterLimit {
		return httpErr.RetryAfter
	}
	attempt := 0
	if job != nil {
		attempt = job.Attempt
	}
	return knowledgeRetryDelay(attempt)
}

func knowledgeRetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 30 * time.Second
	}
	return 2 * time.Minute
}

// knowledgeProviderError turns a provider transport failure into a durable
// worker error. The underlying HTTPError is retained for Retry-After-aware
// scheduling while the stable code is stored in knowledge_job.error_code.
func knowledgeProviderError(callErr error, message string) *Error {
	status := http.StatusBadGateway
	code := classifyProviderError(callErr)
	retryable := false
	var httpErr *llm.HTTPError
	if errors.As(callErr, &httpErr) {
		status = httpErr.StatusCode
		retryable = httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
		switch {
		case httpErr.StatusCode == http.StatusTooManyRequests:
			code = "provider_rate_limited"
		case httpErr.StatusCode >= 500:
			code = "provider_unavailable"
		}
		if httpErr.RetryAfterSet && httpErr.RetryAfter > knowledgeProviderRetryAfterLimit {
			code = "provider_retry_after_too_long"
			retryable = false
		}
	} else if errors.Is(callErr, context.DeadlineExceeded) {
		code = "upstream_timeout"
		retryable = true
	}
	result := knowledgeError(status, code, message, callErr)
	result.Retryable = retryable
	return result
}

func (s *Service) processJob(ctx context.Context, job *workerJob) error {
	switch job.Stage {
	case "fetch":
		return s.processFetch(ctx, job)
	case "parse", "chunk":
		return s.processParse(ctx, job)
	case "embed":
		return s.processEmbed(ctx, job)
	case "extract":
		return s.processExtract(ctx, job)
	case "activate":
		return s.processActivate(ctx, job)
	case "cleanup":
		return s.processCleanup(ctx, job)
	case "enhance":
		return s.processEnhance(ctx, job)
	default:
		return badRequest("invalid_job_stage", "unsupported knowledge job stage")
	}
}

func (s *Service) sourceForVersion(ctx context.Context, versionID pgtype.UUID) (documentID, baseID, objectKey, mimeType, sourceURL string, parsedKey *string, status string, err error) {
	var parsed pgtype.Text
	err = s.db.QueryRow(ctx, `SELECT v.document_id::text,v.knowledge_base_id::text,v.source_object_key,v.mime_type,COALESCE(d.source_url,''),v.parsed_object_key,v.status FROM knowledge_document_version v JOIN knowledge_document d ON d.id=v.document_id WHERE v.id=$1 AND d.deleted_at IS NULL`, versionID).Scan(&documentID, &baseID, &objectKey, &mimeType, &sourceURL, &parsed, &status)
	if err != nil {
		return
	}
	parsedKey = nullableText(parsed)
	return
}

func (s *Service) processFetch(ctx context.Context, job *workerJob) error {
	if job.DocumentVersionID == nil {
		return badRequest("invalid_job", "fetch job has no document version")
	}
	versionID, err := parseID(*job.DocumentVersionID, "version_id")
	if err != nil {
		return err
	}
	documentID, _, _, mimeType, sourceURL, _, _, err := s.sourceForVersion(ctx, versionID)
	if err != nil {
		return internal("failed to load URL version", err)
	}
	base, err := parseID(job.KnowledgeBaseID, "knowledge_base_id")
	if err != nil {
		return err
	}
	workspace, err := parseID(job.WorkspaceID, "workspace_id")
	if err != nil {
		return err
	}
	if strings.TrimSpace(sourceURL) == "" {
		if value, ok := job.Input["source_url"].(string); ok {
			sourceURL = value
		}
	}
	if strings.TrimSpace(sourceURL) == "" {
		return badRequest("invalid_url", "URL source is missing")
	}
	data, fetchedType, err := s.fetchURL(ctx, sourceURL)
	if err != nil {
		return err
	}
	if fetchedType != "" {
		mimeType = fetchedType
	}
	hash := HashBytes(data)
	key := fmt.Sprintf("knowledge/%s/%s/%s/%s/%s", job.WorkspaceID, uuidString(base), uuidString(mustID(documentID)), *job.DocumentVersionID, hash)
	if s.store == nil {
		return unavailable()
	}
	if _, err := s.store.Upload(ctx, key, data, mimeType, filenameFromInput(job.Input, sourceURL)); err != nil {
		return internal("failed to store fetched URL", err)
	}
	cleanup := func() { s.deleteObjectIfUnreferenced(ctx, key) }
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		cleanup()
		return retryableKnowledge(http.StatusServiceUnavailable, "fetch_commit_unavailable", "fetched URL will retry", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, workspace); err != nil {
		cleanup()
		return err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, base).Scan(&lockedBase); err != nil {
		cleanup()
		if errors.Is(err, pgx.ErrNoRows) {
			return conflict("job_cancelled", "the document job was cancelled because its knowledge base was deleted")
		}
		return retryableKnowledge(http.StatusServiceUnavailable, "fetch_commit_unavailable", "fetched URL will retry", err)
	}
	// DeleteBase takes the base lock before cancelling jobs. Holding the same
	// lock here makes source replacement and parse enqueue atomic with respect
	// to deletion.
	if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
		cleanup()
		return err
	}
	if command, err := tx.Exec(ctx, `UPDATE knowledge_document_version SET source_object_key=$2,source_hash=$3,byte_size=$4,mime_type=$5 WHERE id=$1 AND knowledge_base_id=$6`, versionID, key, hash, len(data), mimeType, base); err != nil {
		cleanup()
		return internal("failed to update fetched version", err)
	} else if command.RowsAffected() == 0 {
		cleanup()
		return conflict("job_cancelled", "the document job was cancelled before the source could be committed")
	}
	jobKeyValue := jobKey("parse", *job.DocumentVersionID, "", hash)
	if _, err := s.enqueueJob(ctx, tx, workspace, base, &versionID, nil, "parse", jobKeyValue, map[string]any{"filename": filenameFromInput(job.Input, sourceURL)}); err != nil {
		cleanup()
		return internal("failed to enqueue URL parse", err)
	}
	if err := tx.Commit(ctx); err != nil {
		cleanup()
		return retryableKnowledge(http.StatusServiceUnavailable, "fetch_commit_unavailable", "fetched URL will retry", err)
	}
	s.notifyKnowledgeInvalidation(ctx, job.WorkspaceID, job.KnowledgeBaseID, "")
	return nil
}

func filenameFromInput(input map[string]any, fallback string) string {
	if value, ok := input["filename"].(string); ok && strings.TrimSpace(value) != "" {
		return value
	}
	parsed, _ := url.Parse(fallback)
	if parsed != nil && parsed.Path != "" {
		return parsed.Path
	}
	return "source.html"
}

func (s *Service) fetchURL(ctx context.Context, raw string) ([]byte, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", badRequest("invalid_url", "only absolute http or https URLs are supported")
	}
	if err := rejectPrivateHost(parsed.Hostname()); err != nil {
		return nil, "", badRequest("url_not_allowed", "source URL resolves to a private or local network")
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.fetchTO)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", internal("failed to build URL request", err)
	}
	request.Header.Set("Accept", "text/html,text/plain,application/pdf,application/octet-stream;q=0.5")
	client := *s.secureFetchClient()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := rejectPrivateHost(req.URL.Hostname()); err != nil {
			return err
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, "", retryableKnowledge(http.StatusGatewayTimeout, "upstream_timeout", "source fetch timed out", err)
		}
		return nil, "", retryableKnowledge(http.StatusBadGateway, "upstream_fetch_failed", "source fetch failed", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return nil, "", retryableKnowledge(http.StatusBadGateway, "upstream_fetch_failed", fmt.Sprintf("source returned HTTP %d", response.StatusCode), nil)
		}
		return nil, "", knowledgeError(http.StatusBadGateway, "upstream_fetch_failed", fmt.Sprintf("source returned HTTP %d", response.StatusCode), nil)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, s.maxFetch+1))
	if err != nil {
		return nil, "", internal("failed to read source response", err)
	}
	if int64(len(data)) > s.maxFetch {
		return nil, "", badRequest("source_too_large", "source exceeds the knowledge fetch limit")
	}
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/html"
	}
	return data, contentType, nil
}

// secureFetchClient clones the configured client for each URL fetch and pins
// every direct connection to an IP that passed the private-network check. A
// hostname can change its DNS answer between the initial validation and the
// actual dial; dialing the resolved IP, rather than asking net/http to resolve
// the hostname again, closes that rebinding window. A custom RoundTripper is
// left intact because it is deployment-owned and may implement its own egress
// policy; the default and *http.Transport paths are made direct and fenced.
func (s *Service) secureFetchClient() *http.Client {
	client := *s.fetchClient
	transport, ok := client.Transport.(*http.Transport)
	if client.Transport == nil {
		transport, ok = http.DefaultTransport.(*http.Transport)
	}
	if !ok || transport == nil {
		return &client
	}
	cloned := transport.Clone()
	// A proxy would receive the hostname and perform its own DNS lookup, so the
	// application could not enforce the same private-address check there.
	cloned.Proxy = nil
	cloned.DialTLSContext = nil
	cloned.DialTLS = nil
	baseDial := cloned.DialContext
	if baseDial == nil {
		dialer := &net.Dialer{Timeout: s.fetchTO, KeepAlive: 30 * time.Second}
		baseDial = dialer.DialContext
	}
	cloned.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := publicKnowledgeHostAddresses(host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			conn, dialErr := baseDial(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	client.Transport = cloned
	return &client
}

func rejectPrivateHost(host string) error {
	_, err := publicKnowledgeHostAddresses(host)
	return err
}

func publicKnowledgeHostAddresses(host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("empty host")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if isPrivateAddr(ip) {
			return nil, errors.New("private address")
		}
		return []netip.Addr{ip}, nil
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return nil, errors.New("localhost")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	addresses := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if parsed, parseErr := netip.ParseAddr(ip.String()); parseErr == nil && isPrivateAddr(parsed) {
			return nil, errors.New("private address")
		} else if parseErr == nil {
			addresses = append(addresses, parsed)
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("host has no usable address")
	}
	return addresses, nil
}

func isPrivateAddr(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

func (s *Service) processParse(ctx context.Context, job *workerJob) error {
	if job.DocumentVersionID == nil {
		return badRequest("invalid_job", "parse job has no document version")
	}
	createdParsedKey := ""
	keepParsedObject := false
	defer func() {
		if createdParsedKey != "" && !keepParsedObject && s.store != nil {
			s.deleteObjectIfUnreferenced(ctx, createdParsedKey)
		}
	}()
	versionID, err := parseID(*job.DocumentVersionID, "version_id")
	if err != nil {
		return err
	}
	documentID, baseID, objectKey, mimeType, _, parsedKey, _, err := s.sourceForVersion(ctx, versionID)
	if err != nil {
		return internal("failed to load source for parsing", err)
	}
	var parsed ParsedDocument
	if job.Stage == "chunk" && parsedKey != nil {
		if s.store == nil {
			return unavailable()
		}
		reader, readErr := s.store.GetReader(ctx, *parsedKey)
		if readErr != nil {
			return internal("failed to read parsed document", readErr)
		}
		defer reader.Close()
		data, readErr := io.ReadAll(io.LimitReader(reader, maxKnowledgeArchiveMemberSize+1))
		if readErr != nil {
			return internal("failed to read parsed document", readErr)
		}
		if len(data) > maxKnowledgeArchiveMemberSize {
			return knowledgeError(http.StatusUnprocessableEntity, "parse_output_too_large", "parsed document exceeds the normalized output limit", nil)
		}
		if err = json.Unmarshal(data, &parsed); err != nil {
			return internal("invalid parsed document", err)
		}
		parsed, err = sanitizeParsedDocument(parsed)
		if err != nil {
			return internal("invalid parsed document", err)
		}
	} else {
		if objectKey == "" || s.store == nil {
			return internal("source object is not available", nil)
		}
		reader, readErr := s.store.GetReader(ctx, objectKey)
		if readErr != nil {
			return internal("failed to read source object", readErr)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, s.maxUpload+1))
		_ = reader.Close()
		if readErr != nil {
			return internal("failed to read source object", readErr)
		}
		if int64(len(data)) > s.maxUpload {
			return badRequest("file_too_large", "source exceeds the knowledge upload limit")
		}
		parsed, err = s.parseDocument(ctx, data, filenameFromInput(job.Input, "source"), mimeType)
		if err != nil {
			if errors.Is(err, ErrUnsupportedFormat) {
				return knowledgeError(http.StatusUnsupportedMediaType, "unsupported_format", "source format is not supported", err)
			}
			var typed *Error
			if errors.As(err, &typed) && typed.Code != "knowledge_internal_error" {
				return typed
			}
			return knowledgeError(http.StatusUnprocessableEntity, "parse_failed", "source could not be parsed", err)
		}
	}
	if len(parsed.Blocks) == 0 {
		return knowledgeError(http.StatusUnprocessableEntity, "parse_failed", "source contains no readable text", nil)
	}
	if parsedKey == nil {
		key := fmt.Sprintf("knowledge/%s/%s/%s/parsed.json", job.WorkspaceID, job.KnowledgeBaseID, *job.DocumentVersionID)
		data, marshalErr := json.Marshal(parsed)
		if marshalErr != nil {
			return internal("failed to encode parsed document", marshalErr)
		}
		if len(data) > maxKnowledgeArchiveMemberSize {
			return knowledgeError(http.StatusUnprocessableEntity, "parse_output_too_large", "parsed document exceeds the normalized output limit", nil)
		}
		if _, err := s.store.Upload(ctx, key, data, "application/json", "parsed.json"); err != nil {
			return internal("failed to store parsed document", err)
		}
		createdParsedKey = key
		parsedKey = &key
	}
	chunks := ChunkDocument(parsed)
	if len(chunks) == 0 {
		return knowledgeError(http.StatusUnprocessableEntity, "parse_failed", "source contains no indexable text", nil)
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return err
	}
	enhancementRequested := false
	if job.Stage != "chunk" {
		enhancementRequested, err = s.parseEnhancementEnabled(ctx, job.WorkspaceID, baseID)
		if err != nil {
			return err
		}
	}
	workspace, err := parseID(job.WorkspaceID, "workspace_id")
	if err != nil {
		return err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return err
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return internal("failed to start parse transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, workspace); err != nil {
		return err
	}
	var activeOrBuilding string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(building_index_id,active_index_id)::text FROM knowledge_base WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, base).Scan(&activeOrBuilding); err != nil {
		return internal("failed to lock knowledge base for parsing", err)
	}
	// Deletion transactions lock the base before cancelling jobs. Keep the
	// same order here so a worker cannot deadlock with a concurrent deletion.
	if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
		return err
	}
	var documentTitle string
	if err := tx.QueryRow(ctx, `SELECT title FROM knowledge_document WHERE id=$1 AND knowledge_base_id=$2 AND deleted_at IS NULL`, doc, base).Scan(&documentTitle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		return internal("failed to load parsed document title", err)
	}
	makeCurrent := strings.TrimSpace(activeOrBuilding) == ""
	if _, err = tx.Exec(ctx, `DELETE FROM knowledge_chunk WHERE version_id=$1`, versionID); err != nil {
		return internal("failed to replace parsed chunks", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM knowledge_embedding WHERE version_id=$1`, versionID); err != nil {
		return internal("failed to replace parsed embeddings", err)
	}
	for ordinal, chunk := range chunks {
		refs, _ := json.Marshal(chunk.BlockRefs)
		locator, _ := json.Marshal(chunk.SourceLocator)
		keywordText, titleText, pathText, bodyText := keywordIndexFields(documentTitle, chunk)
		chunkID, _ := newID()
		if _, err = tx.Exec(ctx, `INSERT INTO knowledge_chunk(id,workspace_id,knowledge_base_id,document_id,version_id,ordinal,block_refs,text,source_locator,token_estimate,text_hash,keyword_text,search_vector) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,setweight(to_tsvector('simple',$13),'A') || setweight(to_tsvector('simple',$14),'B') || setweight(to_tsvector('simple',$15),'C'))`, chunkID, workspace, base, doc, versionID, ordinal, refs, chunk.Text, locator, chunk.TokenEstimate, chunk.TextHash, keywordText, titleText, pathText, bodyText); err != nil {
			return internal("failed to store parsed chunks", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE knowledge_document_version SET parsed_object_key=$2,parser_version=$3,chunker_version=$4,status='ready',error_code=NULL WHERE id=$1`, versionID, *parsedKey, parsed.ParserVersion, ChunkerVersion); err != nil {
		return internal("failed to mark parsed version ready", err)
	}
	if makeCurrent {
		if _, err = tx.Exec(ctx, `UPDATE knowledge_document SET current_version_id=$2,revision=revision+1,updated_at=now() WHERE id=$1`, doc, versionID); err != nil {
			return internal("failed to activate parsed document", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE knowledge_base SET corpus_revision=corpus_revision+1,updated_at=now() WHERE id=$1`, base); err != nil {
		return internal("failed to update knowledge corpus revision", err)
	}
	// Follow-up jobs are part of the same commit as the parsed chunks. A
	// process crash after this transaction must not leave a ready version with
	// no embedding/extraction work queued. Include the parse job ID in the key
	// so an explicit reprocess can enqueue a fresh follow-up even when an older
	// run for the same version already succeeded.
	if enhancementRequested {
		if _, err = s.enqueueJob(ctx, tx, workspace, base, &versionID, nil, "enhance", jobKey("enhance", *job.DocumentVersionID, "", "after:"+job.ID), map[string]any{"version_id": *job.DocumentVersionID}); err != nil {
			return internal("failed to enqueue parsed enhancement", err)
		}
	} else if activeOrBuilding != "" {
		index, parseErr := parseID(activeOrBuilding, "index_id")
		if parseErr != nil {
			return parseErr
		}
		if _, err = s.enqueueJob(ctx, tx, workspace, base, &versionID, &index, "embed", jobKey("embed", *job.DocumentVersionID, activeOrBuilding, "after:"+job.ID), map[string]any{"version_id": *job.DocumentVersionID}); err != nil {
			return internal("failed to enqueue parsed embedding", err)
		}
	}
	if !enhancementRequested {
		if _, err = s.enqueueJob(ctx, tx, workspace, base, &versionID, nil, "extract", jobKey("extract", *job.DocumentVersionID, "", "after:"+job.ID), nil); err != nil {
			return internal("failed to enqueue parsed extraction", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE knowledge_job SET progress=jsonb_build_object('chunks',$2,'blocks',$3) WHERE id=$1 AND lease_token=$4`, mustID(job.ID), len(chunks), len(parsed.Blocks), job.LeaseToken); err != nil {
		return internal("failed to update parse progress", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("failed to commit parsed version", err)
	}
	keepParsedObject = true
	s.notifyKnowledgeInvalidation(ctx, job.WorkspaceID, job.KnowledgeBaseID, "")
	return nil
}

func (b ModelBinding) ModelString() string {
	if b.Model == nil {
		return ""
	}
	return *b.Model
}

func (s *Service) processEmbed(ctx context.Context, job *workerJob) error {
	if job.DocumentVersionID == nil || job.IndexID == nil {
		return badRequest("invalid_job", "embed job is missing a version or index")
	}
	binding, dimension, err := s.resolveIndexEmbedding(ctx, job.WorkspaceID, job.KnowledgeBaseID, *job.IndexID)
	if err != nil {
		return err
	}
	index, err := parseID(*job.IndexID, "index_id")
	if err != nil {
		return err
	}
	version, err := parseID(*job.DocumentVersionID, "version_id")
	if err != nil {
		return err
	}
	base, err := parseID(job.KnowledgeBaseID, "knowledge_base_id")
	if err != nil {
		return err
	}
	rows, err := s.db.Query(ctx, `SELECT c.id::text,c.text,d.title FROM knowledge_chunk c JOIN knowledge_document d ON d.id=c.document_id WHERE c.version_id=$1 AND c.knowledge_base_id=$2 AND d.deleted_at IS NULL ORDER BY c.ordinal`, version, base)
	if err != nil {
		return internal("failed to load chunks for embedding", err)
	}
	defer rows.Close()
	type item struct{ id, text string }
	items := []item{}
	for rows.Next() {
		var id, text, title string
		if scanErr := rows.Scan(&id, &text, &title); scanErr != nil {
			return internal("failed to read chunk for embedding", scanErr)
		}
		items = append(items, item{id: id, text: title + "\n" + text})
	}
	if err := rows.Err(); err != nil {
		return internal("failed to load chunks for embedding", err)
	}
	if len(items) == 0 {
		return nil
	}
	client := s.compatibleClient(binding)
	for start := 0; start < len(items); start += 32 {
		end := start + 32
		if end > len(items) {
			end = len(items)
		}
		inputs := make([]string, end-start)
		for i := range inputs {
			inputs[i] = items[start+i].text
		}
		if err := fenceKnowledgeJob(ctx, s.db, job, false); err != nil {
			return err
		}
		vectors, callErr := client.Embeddings(ctx, *binding.Binding.Model, inputs)
		if callErr != nil {
			return knowledgeProviderError(callErr, "embedding provider failed")
		}
		if len(vectors) != len(inputs) {
			return knowledgeError(http.StatusBadGateway, "invalid_embedding_response", "embedding provider returned a different number of vectors than requested", nil)
		}
		tx, txErr := s.txStarter.Begin(ctx)
		if txErr != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "embedding_write_unavailable", "embedding results will retry", txErr)
		}
		if err := lockKnowledgeWorkspace(ctx, tx, mustID(job.WorkspaceID)); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		var lockedBase pgtype.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, base).Scan(&lockedBase); err != nil {
			_ = tx.Rollback(ctx)
			return internal("failed to lock knowledge base for embedding", err)
		}
		if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		for i, values := range vectors {
			literal, literalErr := VectorLiteral(values, dimension)
			if literalErr != nil {
				_ = tx.Rollback(ctx)
				return knowledgeError(http.StatusUnprocessableEntity, "embedding_dimension_mismatch", "embedding dimension does not match the index", literalErr)
			}
			chunkID, parseErr := parseID(items[start+i].id, "chunk_id")
			if parseErr != nil {
				_ = tx.Rollback(ctx)
				return parseErr
			}
			if _, err := tx.Exec(ctx, `INSERT INTO knowledge_embedding(id,workspace_id,knowledge_base_id,index_id,chunk_id,version_id,dimension,embedding) VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector) ON CONFLICT(index_id,chunk_id) DO UPDATE SET dimension=EXCLUDED.dimension,embedding=EXCLUDED.embedding`, newIDValue(), mustID(job.WorkspaceID), base, index, chunkID, version, dimension, literal); err != nil {
				_ = tx.Rollback(ctx)
				return internal("failed to store embedding", err)
			}
		}
		progress, _ := jsonBytes(map[string]any{"embedded": end, "total": len(items)})
		if _, err := tx.Exec(ctx, `UPDATE knowledge_job SET progress=$3 WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()`, mustID(job.ID), job.LeaseToken, progress); err != nil {
			_ = tx.Rollback(ctx)
			return internal("failed to update embedding progress", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "embedding_write_unavailable", "embedding results will retry", err)
		}
	}
	// Pointer promotion and activation enqueue are fenced in the same short
	// transaction. A reclaimed worker can therefore neither publish a late
	// current version nor create the follow-up activation from an old lease.
	tx, txErr := s.txStarter.Begin(ctx)
	if txErr != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "embedding_write_unavailable", "embedding results will retry", txErr)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, mustID(job.WorkspaceID)); err != nil {
		return err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, base).Scan(&lockedBase); err != nil {
		return internal("failed to lock knowledge base for embedding activation", err)
	}
	if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_document d SET current_version_id=$2,revision=revision+1,updated_at=now() WHERE d.id=(SELECT document_id FROM knowledge_document_version WHERE id=$1 AND knowledge_base_id=$3) AND d.knowledge_base_id=$3 AND d.deleted_at IS NULL AND EXISTS (SELECT 1 FROM knowledge_index i WHERE i.id=$4 AND i.knowledge_base_id=$3 AND i.status='active') AND (d.current_version_id IS NULL OR (SELECT version_number FROM knowledge_document_version WHERE id=d.current_version_id)<(SELECT version_number FROM knowledge_document_version WHERE id=$1))`, version, version, base, index); err != nil {
		return internal("failed to activate embedded version", err)
	}
	if _, err := s.enqueueJob(ctx, tx, mustID(job.WorkspaceID), base, nil, &index, "activate", jobKey("activate", "", *job.IndexID, ""), map[string]any{"index_id": *job.IndexID}); err != nil {
		return internal("failed to enqueue index activation", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "embedding_write_unavailable", "embedding results will retry", err)
	}
	s.notifyKnowledgeInvalidation(ctx, job.WorkspaceID, job.KnowledgeBaseID, "")
	return nil
}

type indexEmbeddingSnapshot struct {
	ProviderID     string `json:"provider_id"`
	Model          string `json:"model"`
	Dimension      int    `json:"dimension"`
	SecretRevision int64  `json:"secret_revision"`
}

// resolveIndexEmbedding loads the immutable model snapshot captured when the
// index build began. It intentionally never resolves the current workspace
// embedding setting: doing so would mix vectors from two model/credential
// revisions in one index.
func (s *Service) resolveIndexEmbedding(ctx context.Context, workspaceID, baseID, indexID string) (*ResolvedBinding, int, error) {
	index, err := parseID(indexID, "index_id")
	if err != nil {
		return nil, 0, err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return nil, 0, err
	}
	var status string
	var dimension int
	var raw []byte
	if err := s.db.QueryRow(ctx, `SELECT status,dimension,embedding_snapshot FROM knowledge_index WHERE id=$1 AND knowledge_base_id=$2`, index, base).Scan(&status, &dimension, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, notFound()
		}
		return nil, 0, internal("failed to load embedding index", err)
	}
	if status != IndexBuilding && status != IndexActive {
		return nil, 0, conflict("index_not_available", "embedding index is not available")
	}
	var snapshot indexEmbeddingSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || strings.TrimSpace(snapshot.ProviderID) == "" || strings.TrimSpace(snapshot.Model) == "" || snapshot.Dimension <= 0 || snapshot.Dimension != dimension {
		return nil, 0, conflict("index_snapshot_missing", "embedding index snapshot is invalid; rebuild the index")
	}
	providerID := snapshot.ProviderID
	model := snapshot.Model
	binding, err := s.resolvedBinding(ctx, workspaceID, ModelBinding{WorkspaceID: workspaceID, Purpose: PurposeEmbedding, Mode: "explicit", ProviderID: &providerID, Model: &model})
	if err != nil {
		return nil, 0, err
	}
	if snapshot.SecretRevision > 0 && binding.SecretRevision != snapshot.SecretRevision {
		return nil, 0, conflict("index_snapshot_stale", "the embedding provider key changed; rebuild the index before using it")
	}
	return binding, dimension, nil
}

func newIDValue() pgtype.UUID { id, _ := newID(); return id }

func (s *Service) processActivate(ctx context.Context, job *workerJob) error {
	indexID := job.IndexID
	if indexID == nil {
		if value, ok := job.Input["index_id"].(string); ok {
			indexID = &value
		}
	}
	if indexID == nil {
		return badRequest("invalid_job", "activate job has no index")
	}
	index, err := parseID(*indexID, "index_id")
	if err != nil {
		return err
	}
	base, err := parseID(job.KnowledgeBaseID, "knowledge_base_id")
	if err != nil {
		return err
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, mustID(job.WorkspaceID)); err != nil {
		return err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 FOR UPDATE`, base).Scan(&lockedBase); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	var status string
	var indexCorpusRevision, baseCorpusRevision int64
	var buildingID pgtype.Text
	if err := tx.QueryRow(ctx, `SELECT status,corpus_revision FROM knowledge_index WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, index, base).Scan(&status, &indexCorpusRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if err := tx.QueryRow(ctx, `SELECT corpus_revision,building_index_id::text FROM knowledge_base WHERE id=$1`, base).Scan(&baseCorpusRevision, &buildingID); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
		return err
	}
	if status != IndexBuilding {
		return nil
	}
	if !buildingID.Valid || buildingID.String != *indexID {
		if _, err := tx.Exec(ctx, `UPDATE knowledge_index SET status='failed' WHERE id=$1 AND status='building'`, index); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE knowledge_base SET building_index_id=NULL,revision=revision+1,updated_at=now() WHERE id=$1 AND building_index_id=$2`, base, index); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
		}
		return conflict("index_stale", "the knowledge corpus changed while the index was building; rebuild the index")
	}
	// A build may overlap with a document import or deletion. The base lock
	// serializes that change with this activation, so bring the build snapshot
	// forward and verify that every latest ready version is covered instead of
	// discarding an otherwise usable rebuild.
	if indexCorpusRevision != baseCorpusRevision {
		if _, err := tx.Exec(ctx, `UPDATE knowledge_index SET corpus_revision=$2 WHERE id=$1 AND status='building'`, index, baseCorpusRevision); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
		}
	}
	var pending int
	if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM knowledge_job WHERE knowledge_base_id=$1 AND stage IN ('fetch','parse','chunk','enhance') AND status IN ('queued','running','waiting_config')`, base).Scan(&pending); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if pending > 0 {
		if err := tx.Commit(ctx); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
		}
		job.Status = JobWaitingConfig
		return s.requeueJob(ctx, job, 5*time.Second)
	}
	var missing int
	if err := tx.QueryRow(ctx, `WITH latest_ready AS (
		SELECT DISTINCT ON (v.document_id) v.id,v.document_id
		FROM knowledge_document_version v
		JOIN knowledge_document d ON d.id=v.document_id
		WHERE v.knowledge_base_id=$1 AND v.status='ready' AND d.deleted_at IS NULL
		ORDER BY v.document_id,v.version_number DESC,v.created_at DESC,v.id DESC
	)
	SELECT count(*)::int
	FROM knowledge_chunk c
	JOIN latest_ready v ON v.id=c.version_id
	WHERE c.knowledge_base_id=$1
	  AND NOT EXISTS (SELECT 1 FROM knowledge_embedding e WHERE e.index_id=$2 AND e.chunk_id=c.id)`, base, index).Scan(&missing); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if missing > 0 {
		if err := tx.Commit(ctx); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
		}
		job.Status = JobWaitingConfig
		return s.requeueJob(ctx, job, 5*time.Second)
	}
	if _, err := tx.Exec(ctx, `WITH latest_ready AS (
		SELECT DISTINCT ON (v.document_id) v.document_id,v.id AS version_id
		FROM knowledge_document_version v
		JOIN knowledge_document d ON d.id=v.document_id
		WHERE v.knowledge_base_id=$1 AND v.status='ready' AND d.deleted_at IS NULL
		ORDER BY v.document_id,v.version_number DESC,v.created_at DESC,v.id DESC
	)
	UPDATE knowledge_document d
	SET current_version_id=latest_ready.version_id,revision=revision+1,updated_at=now()
	FROM latest_ready
	WHERE d.id=latest_ready.document_id
	  AND d.knowledge_base_id=$1
	  AND d.deleted_at IS NULL
	  AND d.current_version_id IS DISTINCT FROM latest_ready.version_id`, base); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_base SET active_index_id=$2,building_index_id=NULL,revision=revision+1,updated_at=now() WHERE id=$1 AND building_index_id=$2`, base, index); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_index SET status='retired' WHERE knowledge_base_id=$1 AND id<>$2 AND status='active'`, base, index); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_index SET status='active',activated_at=now() WHERE id=$1 AND status='building'`, index); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "index_activation_unavailable", "embedding index activation will retry", err)
	}
	s.notifyKnowledgeInvalidation(ctx, job.WorkspaceID, job.KnowledgeBaseID, "")
	return nil
}

func (s *Service) processCleanup(ctx context.Context, job *workerJob) error {
	return s.finishCleanup(ctx, job)
}
func (s *Service) finishCleanup(ctx context.Context, job *workerJob) error {
	if job == nil {
		return badRequest("invalid_job", "cleanup job is missing")
	}
	if s.store == nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_storage_unavailable", "private knowledge storage is unavailable", nil)
	}
	if err := fenceKnowledgeJob(ctx, s.db, job, false); err != nil {
		return err
	}
	keys := make([]string, 0)
	if raw, ok := job.Input["object_keys"].([]any); ok {
		for _, value := range raw {
			if key, ok := value.(string); ok && strings.TrimSpace(key) != "" {
				keys = append(keys, key)
			}
		}
	}
	if raw, ok := job.Input["object_keys"].([]string); ok {
		for _, key := range raw {
			if strings.TrimSpace(key) != "" {
				keys = append(keys, key)
			}
		}
	}
	deleteObjects := func() error {
		for _, key := range keys {
			if err := fenceKnowledgeJob(ctx, s.db, job, false); err != nil {
				return err
			}
			if err := s.deleteObjectIfUnreferenced(ctx, key); err != nil {
				return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_object_failed", "private knowledge object cleanup will retry", err)
			}
		}
		return nil
	}
	currentJob, err := parseID(job.ID, "job_id")
	if err != nil {
		return err
	}
	if scope, _ := job.Input["scope"].(string); scope == "workspace" {
		if err := deleteObjects(); err != nil {
			return err
		}
		tx, err := s.txStarter.Begin(ctx)
		if err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_unavailable", "knowledge cleanup will retry", err)
		}
		defer tx.Rollback(ctx)
		if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
			return err
		}
		command, err := tx.Exec(ctx, `DELETE FROM knowledge_job WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()`, currentJob, job.LeaseToken)
		if err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", err)
		}
		if command.RowsAffected() != 1 {
			return conflict("job_cancelled", "the knowledge cleanup job is no longer running")
		}
		if err := tx.Commit(ctx); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", err)
		}
		return nil
	}
	base, err := parseID(job.KnowledgeBaseID, "knowledge_base_id")
	if err != nil {
		return err
	}
	if scope, _ := job.Input["scope"].(string); scope == "document" {
		documentID, ok := job.Input["document_id"].(string)
		if !ok || strings.TrimSpace(documentID) == "" {
			return badRequest("invalid_cleanup_job", "document cleanup job has no document")
		}
		doc, err := parseID(documentID, "document_id")
		if err != nil {
			return err
		}
		tx, err := s.txStarter.Begin(ctx)
		if err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_unavailable", "knowledge cleanup will retry", err)
		}
		defer tx.Rollback(ctx)
		if err := lockKnowledgeWorkspace(ctx, tx, mustID(job.WorkspaceID)); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_unavailable", "knowledge cleanup will retry", err)
		}
		var lockedBase pgtype.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 FOR UPDATE`, base).Scan(&lockedBase); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_unavailable", "knowledge cleanup will retry", err)
		}
		if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
			return err
		}
		for _, statement := range []string{
			`DELETE FROM knowledge_embedding WHERE knowledge_base_id=$1 AND chunk_id IN (SELECT id FROM knowledge_chunk WHERE document_id=$2)`,
			`DELETE FROM knowledge_evidence WHERE knowledge_base_id=$1 AND version_id IN (SELECT id FROM knowledge_document_version WHERE document_id=$2)`,
			`DELETE FROM knowledge_extraction_run WHERE knowledge_base_id=$1 AND version_id IN (SELECT id FROM knowledge_document_version WHERE document_id=$2)`,
			`DELETE FROM knowledge_job WHERE knowledge_base_id=$1 AND document_version_id IN (SELECT id FROM knowledge_document_version WHERE document_id=$2) AND id<>$3`,
			`DELETE FROM knowledge_chunk WHERE knowledge_base_id=$1 AND document_id=$2`,
			`DELETE FROM knowledge_document_version WHERE knowledge_base_id=$1 AND document_id=$2`,
			`DELETE FROM knowledge_document WHERE knowledge_base_id=$1 AND id=$2 AND deleted_at IS NOT NULL`,
		} {
			var execErr error
			if strings.Contains(statement, "$3") {
				_, execErr = tx.Exec(ctx, statement, base, doc, currentJob)
			} else {
				_, execErr = tx.Exec(ctx, statement, base, doc)
			}
			if execErr != nil {
				return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", execErr)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", err)
		}
		if err := deleteObjects(); err != nil {
			return err
		}
		return nil
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_unavailable", "knowledge cleanup will retry", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, mustID(job.WorkspaceID)); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_unavailable", "knowledge cleanup will retry", err)
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 FOR UPDATE`, base).Scan(&lockedBase); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_unavailable", "knowledge cleanup will retry", err)
	}
	if err := fenceKnowledgeJob(ctx, tx, job, true); err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM knowledge_embedding WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_evidence WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_relation WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_entity_alias WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_entity WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_extraction_run WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_chunk WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_document_version WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_document WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_index WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_model_binding WHERE knowledge_base_id=$1`,
		`DELETE FROM knowledge_graph_edit WHERE knowledge_base_id=$1`,
	} {
		if _, err := tx.Exec(ctx, statement, base); err != nil {
			return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM knowledge_job WHERE knowledge_base_id=$1 AND id<>$2`, base, currentJob); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_base SET active_index_id=NULL,building_index_id=NULL,updated_at=now() WHERE id=$1 AND deleted_at IS NOT NULL`, base); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return retryableKnowledge(http.StatusServiceUnavailable, "cleanup_database_failed", "knowledge cleanup will retry", err)
	}
	if err := deleteObjects(); err != nil {
		return err
	}
	return nil
}
