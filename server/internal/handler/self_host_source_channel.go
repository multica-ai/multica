package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/sourcechannel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const selfHostSourceChannelBodyLimit = 4 * 1024

// RecordSelfHostSourceChannel receives the anonymous source-channel report
// posted by self-hosted Multica instances. The payload deliberately excludes
// account/profile/workspace data; only a fixed channel enum, optional
// "other" text, an optional reporting domain, and dedupe hashes are accepted.
func (h *Handler) RecordSelfHostSourceChannel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, selfHostSourceChannelBodyLimit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req sourcechannel.Report
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	channel := sourcechannel.NormalizeChannel(req.Channel)
	instanceHash := sourcechannel.NormalizeHash(req.InstanceHash)
	subjectHash := sourcechannel.NormalizeHash(req.SubjectHash)
	sourceOther := sourcechannel.NormalizeSourceOther(channel, req.SourceOther)
	domain := ""
	if req.Domain != nil {
		domain = sourcechannel.NormalizeDomain(*req.Domain)
	}
	domainMD5 := sourcechannel.NormalizeHash(req.DomainMD5)
	if req.SchemaVersion != sourcechannel.SchemaVersion {
		writeError(w, http.StatusBadRequest, "unsupported schema_version")
		return
	}
	if !sourcechannel.ValidChannel(channel) {
		writeError(w, http.StatusBadRequest, "invalid channel")
		return
	}
	if !sourcechannel.ValidHash(instanceHash) {
		writeError(w, http.StatusBadRequest, "invalid instance_hash")
		return
	}
	if !sourcechannel.ValidHash(subjectHash) {
		writeError(w, http.StatusBadRequest, "invalid subject_hash")
		return
	}
	if req.Domain != nil && (domain == "" || sourcechannel.IsOfficialMulticaAPIURL(domain)) {
		writeError(w, http.StatusBadRequest, "invalid domain")
		return
	}
	if domainMD5 == "" || !sourcechannel.ValidDomainMD5(domainMD5) {
		writeError(w, http.StatusBadRequest, "invalid domain_md5")
		return
	}
	if domain != "" && domainMD5 != sourcechannel.DomainMD5(domain) {
		writeError(w, http.StatusBadRequest, "invalid domain_md5")
		return
	}

	params := db.UpsertSelfHostSourceChannelParams{
		SchemaVersion: int32(req.SchemaVersion),
		Channel:       channel,
		InstanceHash:  instanceHash,
		SubjectHash:   subjectHash,
		DomainMd5:     pgtype.Text{String: domainMD5, Valid: true},
	}
	if domain != "" {
		params.Domain = pgtype.Text{String: domain, Valid: true}
	}
	if sourceOther != "" {
		params.SourceOther = pgtype.Text{String: sourceOther, Valid: true}
	}
	_, err := h.Queries.UpsertSelfHostSourceChannel(r.Context(), params)
	if err != nil {
		slog.Warn("self-host source channel upsert failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to record source channel")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
