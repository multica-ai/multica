package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/pluginbundled"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
	"golang.org/x/sync/singleflight"
)

type PluginService struct {
	Queries               *db.Queries
	TxStarter             TxStarter
	BundledCatalog        *pluginbundled.Catalog
	RemoteMCPSecrets      RemoteMCPSecretBox
	remoteMCPOAuthRefresh singleflight.Group
}

const (
	MaxWorkspacePrivatePluginReleases = int64(100)
	MaxWorkspacePrivatePluginBytes    = int64(256 << 20)
	privatePluginPublisherType        = "private_dev"
	privatePluginTrustTier            = "private_dev"
)

func NewPluginService(queries *db.Queries, txStarter TxStarter) *PluginService {
	return &PluginService{
		Queries:        queries,
		TxStarter:      txStarter,
		BundledCatalog: pluginbundled.Load(),
	}
}

type PluginErrorKind string

const (
	PluginErrorInvalid      PluginErrorKind = "invalid"
	PluginErrorNotFound     PluginErrorKind = "not_found"
	PluginErrorConflict     PluginErrorKind = "conflict"
	PluginErrorIncompatible PluginErrorKind = "incompatible"
)

type PluginError struct {
	Kind    PluginErrorKind
	Message string
	Err     error
}

func (e *PluginError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *PluginError) Unwrap() error { return e.Err }

func newPluginError(kind PluginErrorKind, message string, err error) error {
	return &PluginError{Kind: kind, Message: message, Err: err}
}

func (s *PluginService) CatalogEntries() []pluginbundled.CatalogEntry {
	return s.BundledCatalog.List()
}

func (s *PluginService) CatalogDiagnostics() []pluginbundled.Diagnostic {
	return s.BundledCatalog.Diagnostics()
}

func (s *PluginService) FindCatalogRelease(pluginKey, version string) (pluginbundled.CatalogEntry, bool) {
	if version == "" {
		return s.BundledCatalog.Latest(pluginKey)
	}
	return s.BundledCatalog.Find(pluginKey, version)
}

func (s *PluginService) InstallCatalogRelease(ctx context.Context, workspaceID, actorID pgtype.UUID, pluginKey, version string) (db.PluginInstallation, error) {
	entry, ok := s.FindCatalogRelease(pluginKey, version)
	if !ok {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin release not found", nil)
	}
	if !entry.Compatible {
		return db.PluginInstallation{}, newPluginError(PluginErrorIncompatible, "Plugin release is incompatible with this Multica version", nil)
	}
	return s.InstallPluginRelease(ctx, workspaceID, actorID, PluginReleasePublication{
		Release:       entry.Release,
		PublisherType: entry.PublisherType,
		TrustTier:     entry.TrustTier,
	})
}

// InstallPrivateArchive is the authenticated workspace acquisition boundary.
// The Server always revalidates uploaded bytes and derives a privacy-safe
// source reference rather than accepting a developer-machine path.
func (s *PluginService) InstallPrivateArchive(ctx context.Context, workspaceID, actorID pgtype.UUID, archive []byte) (db.PluginInstallation, error) {
	archiveDigest := plugincontract.DigestBytes(archive)
	release, err := plugincontract.ValidateReleaseCandidate(plugincontract.ReleaseCandidate{
		Archive:    archive,
		SourceKind: plugincontract.SourcePrivateDev,
		SourceRef:  "private://" + archiveDigest,
	}, nil)
	if err != nil {
		return db.PluginInstallation{}, newPluginError(PluginErrorInvalid, "Private Plugin package is invalid", err)
	}
	if compatible, _ := pluginbundled.CompatibleWithV1Host(release.Manifest); !compatible {
		return db.PluginInstallation{}, newPluginError(PluginErrorIncompatible, "Private Plugin release is incompatible with this Multica version", nil)
	}
	return s.InstallPluginRelease(ctx, workspaceID, actorID, PluginReleasePublication{
		Release:       release,
		PublisherType: privatePluginPublisherType,
		TrustTier:     privatePluginTrustTier,
	})
}

func (s *PluginService) UpgradeCatalogRelease(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, pluginKey, version string) (db.PluginInstallation, error) {
	entry, ok := s.FindCatalogRelease(pluginKey, version)
	if !ok {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin release not found", nil)
	}
	if !entry.Compatible {
		return db.PluginInstallation{}, newPluginError(PluginErrorIncompatible, "Plugin release is incompatible with this Multica version", nil)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	installation, err := q.GetPluginInstallation(ctx, installationID)
	if err != nil || installation.WorkspaceID != workspaceID {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	if installation.SourceKind != plugincontract.SourceBundled {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	identity, err := q.GetPluginIdentity(ctx, installation.PluginID)
	if err != nil || identity.PluginKey != pluginKey {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	currentRelease, err := q.GetPluginRelease(ctx, installation.DesiredReleaseID)
	if err != nil {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin installation release not found", nil)
	}
	if !pluginbundled.IsNewerVersion(entry.Release.Manifest.Metadata.Version, currentRelease.Version) {
		return db.PluginInstallation{}, newPluginError(PluginErrorConflict, "Target Plugin release is not newer than the installed release", nil)
	}
	_, release, err := ensurePluginRelease(ctx, q, workspaceID, PluginReleasePublication{
		Release:       entry.Release,
		PublisherType: entry.PublisherType,
		TrustTier:     entry.TrustTier,
	})
	if err != nil {
		return db.PluginInstallation{}, err
	}
	installation, err = q.SetPluginInstallationDesiredState(ctx, db.SetPluginInstallationDesiredStateParams{
		Enabled:          installation.Enabled,
		UpdatedBy:        actorID,
		WorkspaceID:      workspaceID,
		DesiredReleaseID: release.ID,
		ID:               installationID,
	})
	if err != nil {
		return db.PluginInstallation{}, fmt.Errorf("set Plugin desired release: %w", err)
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return db.PluginInstallation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PluginInstallation{}, err
	}
	return s.Queries.GetPluginInstallation(ctx, installationID)
}

// PluginReleasePublication carries the trust decision made by the acquisition
// boundary alongside a release already validated by plugincontract.
type PluginReleasePublication struct {
	Release       plugincontract.ValidatedRelease
	PublisherType string
	TrustTier     string
}

// InstallPluginRelease publishes an already validated release into the
// immutable registry (idempotently), installs it disabled, and activates an
// empty snapshot. Acquisition, signature trust roots, and user-facing catalogs
// intentionally live outside the lifecycle foundation.
func (s *PluginService) InstallPluginRelease(ctx context.Context, workspaceID, actorID pgtype.UUID, publication PluginReleasePublication) (db.PluginInstallation, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)

	identity, release, err := ensurePluginRelease(ctx, q, workspaceID, publication)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	existing, existingErr := q.GetWorkspacePluginInstallation(ctx, db.GetWorkspacePluginInstallationParams{
		WorkspaceID: workspaceID,
		PluginID:    identity.ID,
	})
	if existingErr == nil {
		if publication.Release.SourceKind != plugincontract.SourcePrivateDev {
			return db.PluginInstallation{}, newPluginError(PluginErrorConflict, "Plugin is already installed", nil)
		}
		auditAction := "plugin_private_uploaded"
		needsReconcile := false
		if existing.DesiredReleaseID != release.ID {
			currentRelease, currentErr := q.GetPluginRelease(ctx, existing.DesiredReleaseID)
			if currentErr != nil {
				return db.PluginInstallation{}, fmt.Errorf("load installed Private Plugin release: %w", currentErr)
			}
			if !pluginbundled.IsNewerVersion(release.Version, currentRelease.Version) {
				return db.PluginInstallation{}, newPluginError(PluginErrorConflict, "Target Private Plugin release is not newer than the installed release; use rollback for an older version", nil)
			}
			existing, err = q.SetPluginInstallationDesiredState(ctx, db.SetPluginInstallationDesiredStateParams{
				Enabled:          existing.Enabled,
				UpdatedBy:        actorID,
				WorkspaceID:      workspaceID,
				DesiredReleaseID: release.ID,
				ID:               existing.ID,
			})
			if err != nil {
				return db.PluginInstallation{}, fmt.Errorf("update private Plugin release: %w", err)
			}
			needsReconcile = true
			auditAction = "plugin_private_upgraded"
		}
		// Private Plugin uploads are the capability-approval surface: a fresh
		// install grants every requested capability below. Keep upgrades
		// consistent by granting capabilities newly introduced by the release.
		// Existing denied grants remain denied; an upload must not silently
		// override an explicit revocation. This also repairs installations made
		// by older servers that activated the contribution metadata but omitted
		// its new grant, leaving the Skill visible in status yet absent at run
		// time.
		grantsChanged, err := grantMissingPrivatePluginCapabilities(
			ctx,
			q,
			workspaceID,
			existing.ID,
			actorID,
			publication.Release.Manifest.RequestedCapabilities,
		)
		if err != nil {
			return db.PluginInstallation{}, err
		}
		if needsReconcile || grantsChanged {
			if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
				return db.PluginInstallation{}, err
			}
		}
		if err := createPluginAudit(ctx, q, workspaceID, actorID, auditAction, identity, release, existing); err != nil {
			return db.PluginInstallation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return db.PluginInstallation{}, err
		}
		return s.Queries.GetPluginInstallation(ctx, existing.ID)
	} else if !errors.Is(existingErr, pgx.ErrNoRows) {
		return db.PluginInstallation{}, existingErr
	}

	installation, err := q.CreatePluginInstallation(ctx, db.CreatePluginInstallationParams{
		InstalledBy: actorID,
		PluginID:    identity.ID,
		ReleaseID:   release.ID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.PluginInstallation{}, fmt.Errorf("create plugin installation: %w", err)
	}
	for _, capability := range publication.Release.Manifest.RequestedCapabilities {
		if _, err := q.CreatePluginGrantRevision(ctx, db.CreatePluginGrantRevisionParams{
			Capability:     capability,
			Decision:       "granted",
			Limits:         []byte(`{}`),
			ApprovedBy:     actorID,
			InstallationID: installation.ID,
			WorkspaceID:    workspaceID,
		}); err != nil {
			return db.PluginInstallation{}, fmt.Errorf("create plugin grant: %w", err)
		}
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return db.PluginInstallation{}, err
	}
	if publication.Release.SourceKind == plugincontract.SourcePrivateDev {
		if err := createPluginAudit(ctx, q, workspaceID, actorID, "plugin_private_installed", identity, release, installation); err != nil {
			return db.PluginInstallation{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PluginInstallation{}, err
	}
	return s.Queries.GetPluginInstallation(ctx, installation.ID)
}

func grantMissingPrivatePluginCapabilities(
	ctx context.Context,
	q *db.Queries,
	workspaceID, installationID, actorID pgtype.UUID,
	requested []string,
) (bool, error) {
	latest, err := q.ListLatestPluginGrants(ctx, installationID)
	if err != nil {
		return false, fmt.Errorf("list private Plugin grants: %w", err)
	}
	existing := make(map[string]struct{}, len(latest))
	for _, grant := range latest {
		existing[grant.Capability] = struct{}{}
	}
	changed := false
	for _, capability := range requested {
		if _, ok := existing[capability]; ok {
			continue
		}
		if _, err := q.CreatePluginGrantRevision(ctx, db.CreatePluginGrantRevisionParams{
			Capability:     capability,
			Decision:       "granted",
			Limits:         []byte(`{}`),
			ApprovedBy:     actorID,
			InstallationID: installationID,
			WorkspaceID:    workspaceID,
		}); err != nil {
			return false, fmt.Errorf("grant upgraded private Plugin capability %s: %w", capability, err)
		}
		existing[capability] = struct{}{}
		changed = true
	}
	return changed, nil
}

func ensurePluginRelease(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, publication PluginReleasePublication) (db.PluginIdentity, db.PluginRelease, error) {
	releaseData := publication.Release
	manifest := releaseData.Manifest
	if publication.PublisherType == "" || publication.TrustTier == "" {
		return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorInvalid, "Plugin publisher trust is required", nil)
	}
	if len(releaseData.Files) == 0 {
		return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorInvalid, "Plugin release contains no artifact files", nil)
	}
	privateRelease := releaseData.SourceKind == plugincontract.SourcePrivateDev
	registryLockKey := "official:" + manifest.Metadata.Key
	if privateRelease {
		if publication.PublisherType != privatePluginPublisherType || publication.TrustTier != privatePluginTrustTier {
			return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorInvalid, "Private Plugin trust must be private / unverified", nil)
		}
		registryLockKey = "private:" + util.UUIDToString(workspaceID) + ":" + manifest.Metadata.Key
	}
	if err := q.LockPluginRegistryKey(ctx, registryLockKey); err != nil {
		return db.PluginIdentity{}, db.PluginRelease{}, fmt.Errorf("lock plugin registry key: %w", err)
	}

	var identity db.PluginIdentity
	var err error
	if privateRelease {
		identity, err = q.GetWorkspacePrivatePluginIdentityByKey(ctx, db.GetWorkspacePrivatePluginIdentityByKeyParams{
			WorkspaceID: workspaceID,
			PluginKey:   manifest.Metadata.Key,
		})
	} else {
		identity, err = q.GetOfficialPluginIdentityByKey(ctx, manifest.Metadata.Key)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		ownerWorkspaceID := pgtype.UUID{}
		if privateRelease {
			ownerWorkspaceID = workspaceID
		}
		identity, err = q.CreatePluginIdentity(ctx, db.CreatePluginIdentityParams{
			PluginKey:        manifest.Metadata.Key,
			DisplayName:      manifest.Metadata.Name,
			PublisherID:      manifest.Metadata.Publisher,
			PublisherType:    publication.PublisherType,
			TrustTier:        publication.TrustTier,
			OwnerWorkspaceID: ownerWorkspaceID,
		})
	}
	if err != nil {
		return db.PluginIdentity{}, db.PluginRelease{}, fmt.Errorf("ensure plugin identity: %w", err)
	}
	if identity.PublisherID != manifest.Metadata.Publisher || identity.PublisherType != publication.PublisherType || identity.TrustTier != publication.TrustTier {
		return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorConflict, "Plugin publisher conflicts with the registered identity", nil)
	}

	release, err := q.GetRegisteredPluginReleaseByVersion(ctx, db.GetRegisteredPluginReleaseByVersionParams{
		PluginID: identity.ID,
		Version:  manifest.Metadata.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if privateRelease {
			// Different private Plugin keys use different registry locks, so
			// serialize the workspace-wide quota check as well.
			if lockErr := q.LockPluginRegistryKey(ctx, "private-quota:"+util.UUIDToString(workspaceID)); lockErr != nil {
				return db.PluginIdentity{}, db.PluginRelease{}, fmt.Errorf("lock Private Plugin quota: %w", lockErr)
			}
			usage, usageErr := q.GetWorkspacePrivatePluginUsage(ctx, workspaceID)
			if usageErr != nil {
				return db.PluginIdentity{}, db.PluginRelease{}, usageErr
			}
			if usage.ReleaseCount >= MaxWorkspacePrivatePluginReleases {
				return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorConflict, "Workspace Private Plugin release quota exceeded", nil)
			}
			if usage.TotalBytes+releaseData.ArtifactSize > MaxWorkspacePrivatePluginBytes {
				return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorConflict, "Workspace Private Plugin storage quota exceeded", nil)
			}
		}
		manifestJSON, marshalErr := json.Marshal(manifest)
		if marshalErr != nil {
			return db.PluginIdentity{}, db.PluginRelease{}, marshalErr
		}
		release, err = q.CreatePluginRelease(ctx, db.CreatePluginReleaseParams{
			Version:        manifest.Metadata.Version,
			Manifest:       manifestJSON,
			ManifestDigest: releaseData.ManifestDigest,
			SourceKind:     releaseData.SourceKind,
			SourceRef:      releaseData.SourceRef,
			ArchiveDigest:  releaseData.ArchiveDigest,
			ArtifactRef:    "artifact://" + releaseData.ArtifactDigest,
			ArtifactDigest: releaseData.ArtifactDigest,
			ArtifactSize:   releaseData.ArtifactSize,
			Signature:      releaseData.Signature,
			SignatureKeyID: pgtype.Text{String: releaseData.SignatureKeyID, Valid: releaseData.SignatureKeyID != ""},
			PluginID:       identity.ID,
		})
		if err == nil {
			for _, contribution := range releaseData.Contributions {
				features, marshalErr := json.Marshal(contribution.RequiredDaemonFeatures)
				if marshalErr != nil {
					return db.PluginIdentity{}, db.PluginRelease{}, marshalErr
				}
				if _, err = q.CreatePluginContribution(ctx, db.CreatePluginContributionParams{
					ContributionKey:        contribution.Key,
					Type:                   contribution.Type,
					SchemaVersion:          contribution.SchemaVersion,
					DisplayName:            contribution.DisplayName,
					Description:            contribution.Description,
					EntryPath:              contribution.EntryPath,
					EntryDigest:            contribution.EntryDigest,
					RequiredDaemonFeatures: features,
					Ordinal:                contribution.Ordinal,
					ReleaseID:              release.ID,
				}); err != nil {
					break
				}
			}
		}
		if err == nil {
			for _, file := range releaseData.Files {
				_, err = q.CreatePluginArtifactFile(ctx, db.CreatePluginArtifactFileParams{
					Path:      file.Path,
					Digest:    file.Digest,
					SizeBytes: file.SizeBytes,
					Content:   string(file.Content),
					ReleaseID: release.ID,
				})
				if err != nil {
					break
				}
			}
		}
	}
	if err != nil {
		return db.PluginIdentity{}, db.PluginRelease{}, fmt.Errorf("ensure plugin release: %w", err)
	}
	if release.RevocationStatus != "active" {
		return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorConflict, "Plugin release has been revoked and cannot be installed", nil)
	}
	if release.ManifestDigest != releaseData.ManifestDigest ||
		release.ArtifactDigest != releaseData.ArtifactDigest ||
		(!privateRelease && release.ArchiveDigest != releaseData.ArchiveDigest) {
		return db.PluginIdentity{}, db.PluginRelease{}, newPluginError(PluginErrorConflict, "Plugin release conflicts with immutable registry content", nil)
	}
	return identity, release, nil
}

func createPluginAudit(ctx context.Context, q *db.Queries, workspaceID, actorID pgtype.UUID, action string, identity db.PluginIdentity, release db.PluginRelease, installation db.PluginInstallation) error {
	details, err := json.Marshal(map[string]any{
		"installation_id": util.UUIDToString(installation.ID),
		"plugin_id":       util.UUIDToString(identity.ID),
		"plugin_key":      identity.PluginKey,
		"release_id":      util.UUIDToString(release.ID),
		"version":         release.Version,
		"source_kind":     release.SourceKind,
		"artifact_digest": release.ArtifactDigest,
	})
	if err != nil {
		return err
	}
	if _, err := q.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: workspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     actorID,
		Action:      action,
		Details:     details,
	}); err != nil {
		return fmt.Errorf("create Plugin audit record: %w", err)
	}
	return nil
}

func (s *PluginService) EnablePlugin(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, scopeType string, scopeID pgtype.UUID) (db.PluginInstallation, error) {
	return s.setPluginBinding(ctx, workspaceID, installationID, actorID, scopeType, scopeID, true)
}

func (s *PluginService) DisablePlugin(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, scopeType string, scopeID pgtype.UUID) (db.PluginInstallation, error) {
	return s.setPluginBinding(ctx, workspaceID, installationID, actorID, scopeType, scopeID, false)
}

func (s *PluginService) setPluginBinding(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, scopeType string, scopeID pgtype.UUID, enabled bool) (db.PluginInstallation, error) {
	if scopeType != "workspace" && scopeType != "agent" {
		return db.PluginInstallation{}, newPluginError(PluginErrorInvalid, "scope_type must be workspace or agent", nil)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	installation, err := q.GetPluginInstallation(ctx, installationID)
	if err != nil || installation.WorkspaceID != workspaceID {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	identity, err := q.GetPluginIdentity(ctx, installation.PluginID)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	release, err := q.GetPluginRelease(ctx, installation.DesiredReleaseID)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	if enabled {
		if err := validateRemoteMCPInstallationReady(ctx, q, workspaceID, installation, release); err != nil {
			return db.PluginInstallation{}, err
		}
	}
	if scopeType == "workspace" && scopeID != workspaceID {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin binding target not found", nil)
	}
	if scopeType == "agent" {
		if _, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: scopeID, WorkspaceID: workspaceID}); err != nil {
			return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin binding target not found", nil)
		}
	}
	if _, err := q.CreatePluginBindingRevision(ctx, db.CreatePluginBindingRevisionParams{
		ScopeType:      scopeType,
		ScopeID:        scopeID,
		Enabled:        enabled,
		CreatedBy:      actorID,
		InstallationID: installationID,
		WorkspaceID:    workspaceID,
	}); err != nil {
		return db.PluginInstallation{}, fmt.Errorf("create plugin binding: %w", err)
	}
	bindings, err := q.ListLatestPluginBindings(ctx, installationID)
	if err != nil {
		return db.PluginInstallation{}, fmt.Errorf("list plugin bindings: %w", err)
	}
	globalEnabled := false
	for _, binding := range bindings {
		if binding.Enabled {
			globalEnabled = true
			break
		}
	}
	installation, err = q.SetPluginInstallationDesiredState(ctx, db.SetPluginInstallationDesiredStateParams{
		Enabled:          globalEnabled,
		UpdatedBy:        actorID,
		WorkspaceID:      workspaceID,
		DesiredReleaseID: installation.DesiredReleaseID,
		ID:               installationID,
	})
	if err != nil {
		return db.PluginInstallation{}, fmt.Errorf("set plugin desired state: %w", err)
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return db.PluginInstallation{}, err
	}
	if installation.SourceKind == plugincontract.SourcePrivateDev {
		action := "plugin_private_disabled"
		if enabled {
			action = "plugin_private_enabled"
		}
		if err := createPluginAudit(ctx, q, workspaceID, actorID, action, identity, release, installation); err != nil {
			return db.PluginInstallation{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PluginInstallation{}, err
	}
	return s.Queries.GetPluginInstallation(ctx, installationID)
}

func validateRemoteMCPInstallationReady(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, installation db.PluginInstallation, release db.PluginRelease) error {
	contributions, err := q.ListPluginContributionsByRelease(ctx, release.ID)
	if err != nil {
		return err
	}
	configs, err := q.ListLatestPluginInstallationConfigs(ctx, db.ListLatestPluginInstallationConfigsParams{
		WorkspaceID: workspaceID, InstallationID: installation.ID,
	})
	if err != nil {
		return err
	}
	byContribution := make(map[string]db.PluginInstallationConfig, len(configs))
	for _, config := range configs {
		byContribution[util.UUIDToString(config.ContributionID)] = config
	}
	for _, contribution := range contributions {
		if contribution.Type != plugincontract.ContributionRemoteMCPV1 {
			continue
		}
		config, ok := byContribution[util.UUIDToString(contribution.ID)]
		var approved []pluginruntime.RemoteMCPTool
		if ok {
			_ = json.Unmarshal(config.ApprovedTools, &approved)
		}
		if !ok || !config.ReviewedAt.Valid || len(approved) == 0 || !config.SchemaDigest.Valid {
			return newPluginError(PluginErrorConflict, "Remote MCP configuration and tool review must be complete before enabling", nil)
		}
		if config.AuthType != "none" {
			if !config.SecretRef.Valid {
				return newPluginError(PluginErrorConflict, "Remote MCP credential must be configured before enabling", nil)
			}
			if _, err := q.GetActivePluginRemoteMCPSecret(ctx, db.GetActivePluginRemoteMCPSecretParams{
				ID: config.SecretRef, WorkspaceID: workspaceID, InstallationID: installation.ID, ContributionID: contribution.ID,
			}); err != nil {
				return newPluginError(PluginErrorConflict, "Remote MCP credential must be configured before enabling", nil)
			}
		}
	}
	return nil
}

func (s *PluginService) RollbackPlugin(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID, version string) (db.PluginInstallation, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	installation, err := q.GetPluginInstallation(ctx, installationID)
	if err != nil || installation.WorkspaceID != workspaceID {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	identity, err := q.GetPluginIdentity(ctx, installation.PluginID)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	release, err := q.GetPluginReleaseByVersion(ctx, db.GetPluginReleaseByVersionParams{
		PluginID: installation.PluginID,
		Version:  version,
	})
	if err != nil {
		return db.PluginInstallation{}, newPluginError(PluginErrorNotFound, "Rollback release not found", nil)
	}
	installation, err = q.SetPluginInstallationDesiredState(ctx, db.SetPluginInstallationDesiredStateParams{
		Enabled:          installation.Enabled,
		UpdatedBy:        actorID,
		WorkspaceID:      workspaceID,
		DesiredReleaseID: release.ID,
		ID:               installationID,
	})
	if err != nil {
		return db.PluginInstallation{}, err
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return db.PluginInstallation{}, err
	}
	if installation.SourceKind == plugincontract.SourcePrivateDev {
		if err := createPluginAudit(ctx, q, workspaceID, actorID, "plugin_private_rolled_back", identity, release, installation); err != nil {
			return db.PluginInstallation{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PluginInstallation{}, err
	}
	return s.Queries.GetPluginInstallation(ctx, installationID)
}

// UninstallPlugin prevents future tasks from seeing an installation while
// retaining immutable releases and artifact files for historical pinned Runs.
func (s *PluginService) UninstallPlugin(ctx context.Context, workspaceID, installationID, actorID pgtype.UUID) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.Queries.WithTx(tx)
	installation, err := q.GetPluginInstallation(ctx, installationID)
	if err != nil || installation.WorkspaceID != workspaceID || installation.UninstalledAt.Valid {
		return newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	identity, err := q.GetPluginIdentity(ctx, installation.PluginID)
	if err != nil {
		return err
	}
	release, err := q.GetPluginRelease(ctx, installation.DesiredReleaseID)
	if err != nil {
		return err
	}
	uninstalled, err := q.UninstallPluginInstallation(ctx, db.UninstallPluginInstallationParams{
		UpdatedBy:   actorID,
		ID:          installationID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return newPluginError(PluginErrorNotFound, "Plugin installation not found", nil)
	}
	if _, err := s.reconcileWorkspaceTx(ctx, q, workspaceID); err != nil {
		return err
	}
	auditAction := "plugin_uninstalled"
	if installation.SourceKind == plugincontract.SourcePrivateDev {
		auditAction = "plugin_private_uninstalled"
	}
	if err := createPluginAudit(ctx, q, workspaceID, actorID, auditAction, identity, release, uninstalled); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PluginService) ReconcileWorkspace(ctx context.Context, workspaceID pgtype.UUID) (db.PluginCapabilitySnapshot, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	snapshot, err := s.reconcileWorkspaceTx(ctx, s.Queries.WithTx(tx), workspaceID)
	if err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}
	return snapshot, nil
}

type compilationKey struct {
	installationID string
	contributionID string
}

func (s *PluginService) reconcileWorkspaceTx(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) (db.PluginCapabilitySnapshot, error) {
	if _, err := q.EnsurePluginWorkspaceCapabilityState(ctx, workspaceID); err != nil {
		return db.PluginCapabilitySnapshot{}, fmt.Errorf("ensure plugin capability state: %w", err)
	}
	state, err := q.GetPluginWorkspaceCapabilityStateForUpdate(ctx, workspaceID)
	if err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}
	installations, err := q.ListPluginInstallationsForCompile(ctx, workspaceID)
	if err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}
	rows, err := q.ListPluginCompilationContributions(ctx, workspaceID)
	if err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}

	sourceGenerations := make(map[string]int64, len(installations))
	for _, installation := range installations {
		sourceGenerations[util.UUIDToString(installation.ID)] = installation.DesiredGeneration
	}
	grouped := make(map[compilationKey][]db.ListPluginCompilationContributionsRow)
	keys := make([]compilationKey, 0)
	for _, row := range rows {
		key := compilationKey{util.UUIDToString(row.InstallationID), util.UUIDToString(row.ContributionID)}
		if _, exists := grouped[key]; !exists {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], row)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].installationID != keys[j].installationID {
			return keys[i].installationID < keys[j].installationID
		}
		return keys[i].contributionID < keys[j].contributionID
	})

	entries := make([]pluginruntime.CompiledEntry, 0, len(rows))
	releaseFiles := make(map[string][]db.PluginArtifactFile)
	for _, key := range keys {
		contributionRows := grouped[key]
		row := contributionRows[0]
		if plugincontract.DigestBytes([]byte(row.EntryContent)) != row.EntryDigest {
			return db.PluginCapabilitySnapshot{}, fmt.Errorf("plugin contribution %s failed digest validation", row.ContributionKey)
		}
		var features []string
		if err := json.Unmarshal(row.RequiredDaemonFeatures, &features); err != nil {
			return db.PluginCapabilitySnapshot{}, fmt.Errorf("decode daemon features: %w", err)
		}
		base := pluginruntime.CompiledEntry{
			PluginID:               util.UUIDToString(row.PluginID),
			PluginKey:              row.PluginKey,
			InstallationID:         util.UUIDToString(row.InstallationID),
			ReleaseID:              util.UUIDToString(row.ReleaseID),
			ReleaseVersion:         row.ReleaseVersion,
			ContributionID:         util.UUIDToString(row.ContributionID),
			ContributionKey:        row.ContributionKey,
			ContributionType:       row.ContributionType,
			DisplayName:            row.DisplayName,
			Description:            row.Description,
			SourceKind:             row.SourceKind,
			ArtifactFileID:         util.UUIDToString(row.ArtifactFileID),
			ArtifactRef:            row.ArtifactRef,
			ArtifactDigest:         row.ArtifactDigest,
			EntryPath:              row.EntryPath,
			EntryDigest:            row.EntryDigest,
			RequiredDaemonFeatures: features,
			Ordinal:                row.Ordinal,
		}
		switch row.ContributionType {
		case plugincontract.ContributionAgentSkillV1:
			releaseKey := util.UUIDToString(row.ReleaseID)
			artifactFiles, ok := releaseFiles[releaseKey]
			if !ok {
				artifactFiles, err = q.ListPluginArtifactFilesByRelease(ctx, row.ReleaseID)
				if err != nil {
					return db.PluginCapabilitySnapshot{}, fmt.Errorf("load plugin Skill artifacts: %w", err)
				}
				releaseFiles[releaseKey] = artifactFiles
			}
			pinnedFiles, bundleFiles, err := compilePluginSkillFiles(row.ReleaseID, row.ContributionKey, row.EntryPath, artifactFiles)
			if err != nil {
				return db.PluginCapabilitySnapshot{}, fmt.Errorf("compile plugin Skill %s: %w", row.ContributionKey, err)
			}
			bundleManifest := skillbundle.BuildManifest(skillbundle.Skill{
				ID:          "plugin:" + util.UUIDToString(row.ContributionID),
				Source:      skillbundle.SourcePlugin,
				Name:        row.ContributionKey,
				Description: row.Description,
				Content:     row.EntryContent,
				Files:       bundleFiles,
			})
			base.SkillBundleHash = bundleManifest.Hash
			base.SkillSizeBytes = bundleManifest.SizeBytes
			base.SkillFileCount = bundleManifest.FileCount
			base.SkillFiles = pinnedFiles
		case plugincontract.ContributionRemoteMCPV1:
			if !row.ConfigID.Valid || !row.ConfigRevision.Valid || !row.Endpoint.Valid || !row.AuthType.Valid || !row.FailurePolicy.Valid || !row.SchemaDigest.Valid {
				return db.PluginCapabilitySnapshot{}, fmt.Errorf("remote MCP contribution %s has incomplete reviewed configuration", row.ContributionKey)
			}
			var tools []pluginruntime.RemoteMCPTool
			if err := json.Unmarshal(row.ApprovedTools, &tools); err != nil || len(tools) == 0 {
				return db.PluginCapabilitySnapshot{}, fmt.Errorf("remote MCP contribution %s has invalid approved tools", row.ContributionKey)
			}
			base.ConfigID = util.UUIDToString(row.ConfigID)
			base.ConfigRevision = row.ConfigRevision.Int64
			base.Endpoint = row.Endpoint.String
			base.PublicConfig = append(json.RawMessage(nil), row.PublicConfig...)
			base.SecretRef = util.UUIDToString(row.SecretRef)
			base.AuthType = row.AuthType.String
			base.AuthHeader = row.AuthHeader.String
			base.ApprovedTools = tools
			base.ToolSchemaDigest = row.SchemaDigest.String
			base.FailurePolicy = row.FailurePolicy.String
			var manifest plugincontract.Manifest
			if err := json.Unmarshal([]byte(row.EntryContent), &manifest); err != nil {
				return db.PluginCapabilitySnapshot{}, fmt.Errorf("remote MCP contribution %s has invalid manifest entry", row.ContributionKey)
			}
			foundDeclaration := false
			for _, declaration := range manifest.Contributes.RemoteMCP {
				if declaration.Key == row.ContributionKey {
					base.Transport = declaration.Transport
					base.ProtocolVersions = append([]string(nil), declaration.ProtocolVersions...)
					base.EndpointAllowedHosts = append([]string(nil), declaration.EndpointPolicy.AllowedHosts...)
					foundDeclaration = true
					break
				}
			}
			if !foundDeclaration {
				return db.PluginCapabilitySnapshot{}, fmt.Errorf("remote MCP contribution %s is absent from its manifest entry", row.ContributionKey)
			}
		default:
			return db.PluginCapabilitySnapshot{}, fmt.Errorf("unsupported plugin contribution type %q", row.ContributionType)
		}

		var workspaceBinding *db.ListPluginCompilationContributionsRow
		agentBindings := make([]db.ListPluginCompilationContributionsRow, 0)
		for i := range contributionRows {
			binding := contributionRows[i]
			if !binding.ScopeType.Valid || !binding.ScopeID.Valid || !binding.BindingEnabled.Valid {
				continue
			}
			if binding.ScopeType.String == "workspace" {
				copy := binding
				workspaceBinding = &copy
			} else if binding.ScopeType.String == "agent" {
				agentBindings = append(agentBindings, binding)
			}
		}
		if workspaceBinding != nil && workspaceBinding.BindingEnabled.Bool {
			workspaceEntry := base
			workspaceEntry.ScopeType = "workspace"
			workspaceEntry.ScopeID = util.UUIDToString(workspaceID)
			for _, binding := range agentBindings {
				workspaceEntry.ExcludedAgentIDs = append(workspaceEntry.ExcludedAgentIDs, util.UUIDToString(binding.ScopeID))
			}
			entries = append(entries, workspaceEntry)
		}
		for _, binding := range agentBindings {
			if !binding.BindingEnabled.Bool {
				continue
			}
			agentEntry := base
			agentEntry.ScopeType = "agent"
			agentEntry.ScopeID = util.UUIDToString(binding.ScopeID)
			entries = append(entries, agentEntry)
		}
	}

	digest, err := pluginruntime.SnapshotDigest(sourceGenerations, entries)
	if err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}
	sourceJSON, _ := json.Marshal(sourceGenerations)
	entriesJSON, _ := json.Marshal(entries)
	snapshot, err := q.CreatePluginCapabilitySnapshot(ctx, db.CreatePluginCapabilitySnapshotParams{
		WorkspaceID:       workspaceID,
		Revision:          state.NextRevision,
		SourceGenerations: sourceJSON,
		CompilerVersion:   pluginruntime.CompilerVersion,
		SchemaVersion:     pluginruntime.SchemaVersion,
		SnapshotDigest:    digest,
		CompiledEntries:   entriesJSON,
		Diagnostics:       []byte(`[]`),
	})
	if err != nil {
		return db.PluginCapabilitySnapshot{}, fmt.Errorf("create plugin capability snapshot: %w", err)
	}
	if _, err := q.ActivatePluginWorkspaceCapabilitySnapshot(ctx, db.ActivatePluginWorkspaceCapabilitySnapshotParams{
		ActiveSnapshotID: snapshot.ID,
		ActiveRevision:   snapshot.Revision,
		NextRevision:     snapshot.Revision + 1,
		WorkspaceID:      workspaceID,
	}); err != nil {
		return db.PluginCapabilitySnapshot{}, fmt.Errorf("activate plugin capability snapshot: %w", err)
	}
	activated, err := q.ActivatePluginInstallations(ctx, workspaceID)
	if err != nil {
		return db.PluginCapabilitySnapshot{}, err
	}
	for _, installation := range activated {
		stateName, reason := "healthy", "snapshot_activated"
		if !installation.Enabled {
			stateName, reason = "disabled", "installation_disabled"
		}
		if _, err := q.CreatePluginHealth(ctx, db.CreatePluginHealthParams{
			WorkspaceID:        workspaceID,
			InstallationID:     installation.ID,
			ScopeType:          "workspace",
			ScopeID:            workspaceID,
			State:              stateName,
			ReasonCode:         reason,
			SafeDetail:         "",
			ObservedGeneration: installation.ActiveGeneration,
			LastGoodSnapshotID: snapshot.ID,
		}); err != nil {
			return db.PluginCapabilitySnapshot{}, err
		}
	}
	return snapshot, nil
}
