package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Publishing.
//
// An author uploads an artifact bundle; Multica stores it and serves it. There
// is no source URL, and there is no second way in: what an administrator
// approves and what a reader's browser runs are the same rows in this database.
//
// A published version is immutable. Publishing again creates another version and
// changes nothing for a workspace that already installed one — upgrading is an
// administrator's action, which is the whole reason the two are separate.
//
// Only the frontend artifact moved here. Hook endpoints and MCP servers still
// live on the author's own infrastructure; we do not run third-party backends.

// PluginPackageVersionSummary is one published version as the settings page
// lists it.
type PluginPackageVersionSummary struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Digest      string `json:"digest"`
	SizeBytes   int64  `json:"size_bytes"`
	PublishedAt string `json:"published_at"`
	// Installed marks the version this workspace currently runs, so "which one
	// am I on" is answerable without cross-referencing two lists.
	Installed bool `json:"installed"`
}

// PluginPackageSummary is one publishable plugin identity with its versions,
// newest first.
type PluginPackageSummary struct {
	ID        string                        `json:"id"`
	PluginKey string                        `json:"plugin_key"`
	Name      string                        `json:"name"`
	Versions  []PluginPackageVersionSummary `json:"versions"`
	CreatedAt string                        `json:"created_at"`
}

const pluginTimeFormat = "2006-01-02T15:04:05Z07:00"

// PublishBundle stores an uploaded artifact bundle as a new immutable version.
func (s *PluginService) PublishBundle(ctx context.Context, workspaceID, userID pgtype.UUID, archive []byte) (PluginPackageSummary, error) {
	bundle, err := plugincontract.ParseBundle(archive)
	if err != nil {
		// The wrapped text names the file or field at fault, and it is written
		// for the author. A publish failure they cannot act on would send them
		// back to guessing, which is the failure mode this whole path replaces.
		return PluginPackageSummary{}, pluginErrf(PluginErrorInvalid, "plugin package is invalid: %v", err)
	}
	return s.publish(ctx, workspaceID, userID, bundle, false)
}

// PublishLocalBundle publishes from MULTICA_PLUGIN_DIR — the development
// channel, so an author iterating on a surface does not have to zip and upload
// after every edit.
//
// It produces an ordinary immutable version rather than a live directory the
// panel reads at render time. A second render path would mean "is the code
// frozen?" depended on how the plugin was installed, which is exactly the
// ambiguity this change removes. Re-publishing an unchanged version string is
// what a dev loop does constantly, so those get a `+dev.N` build suffix instead
// of a conflict.
func (s *PluginService) PublishLocalBundle(ctx context.Context, workspaceID, userID pgtype.UUID, name string) (PluginPackageSummary, error) {
	if s.LocalDir == "" {
		return PluginPackageSummary{}, pluginErrf(PluginErrorInvalid, "local plugin sources require MULTICA_PLUGIN_DIR")
	}
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return PluginPackageSummary{}, pluginErrf(PluginErrorInvalid, "local plugin source must be a single directory name under MULTICA_PLUGIN_DIR")
	}
	bundle, err := plugincontract.ParseBundleFromDir(func(entry string) ([]byte, bool, error) {
		content, readErr := s.readLocalFile(name, entry)
		var pluginErr *PluginError
		if errors.As(readErr, &pluginErr) && pluginErr.Kind == PluginErrorNotFound {
			return nil, false, nil
		}
		if readErr != nil {
			return nil, false, readErr
		}
		return content, true, nil
	})
	if err != nil {
		return PluginPackageSummary{}, pluginErrf(PluginErrorInvalid, "local plugin package is invalid: %v", err)
	}
	return s.publish(ctx, workspaceID, userID, bundle, true)
}

// publish writes the package row, the version and its files in ONE transaction,
// under the (workspace, plugin key) lock.
//
// All three parts are in the same transaction for the same reason: any partial
// result is a lie the version list cannot show. A version whose files half
// landed is a panel that loads nothing; a package row whose display name was
// updated by a publish that then conflicted claims to describe a version that
// was never stored; and a first publish that failed after creating the package
// leaves a plugin with zero versions in the list.
func (s *PluginService) publish(ctx context.Context, workspaceID, userID pgtype.UUID, bundle plugincontract.Bundle, devLoop bool) (PluginPackageSummary, error) {
	if err := bundle.Manifest.CheckCapabilities(s.Host); err != nil {
		return PluginPackageSummary{}, &PluginError{Kind: PluginErrorIncompatible, Message: capabilityMessage(err), Err: err}
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return PluginPackageSummary{}, &PluginError{Kind: PluginErrorUnavailable, Message: "begin publish", Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.Queries.WithTx(tx)

	if err := lockPluginPackageKey(ctx, queries, workspaceID, bundle.Manifest.Key); err != nil {
		return PluginPackageSummary{}, err
	}
	pkg, err := upsertPackage(ctx, queries, workspaceID, userID, bundle.Manifest)
	if err != nil {
		return PluginPackageSummary{}, err
	}
	existing, err := queries.ListPluginPackageVersions(ctx, pkg.ID)
	if err != nil {
		return PluginPackageSummary{}, &PluginError{Kind: PluginErrorUnavailable, Message: "list published versions", Err: err}
	}
	version, err := resolvePublishVersion(bundle.Manifest.Version, existing, devLoop)
	if err != nil {
		return PluginPackageSummary{}, err
	}

	row, err := queries.CreatePluginPackageVersion(ctx, db.CreatePluginPackageVersionParams{
		PackageID:   pkg.ID,
		WorkspaceID: workspaceID,
		Version:     version,
		Manifest:    bundle.Canonical,
		Digest:      BundleDigest(bundle),
		SizeBytes:   int64(bundle.TotalSize()),
		PublishedBy: userID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return PluginPackageSummary{}, pluginErrf(PluginErrorConflict, "version %s of this plugin is already published; published versions are immutable, so publish a new version instead", version)
		}
		return PluginPackageSummary{}, &PluginError{Kind: PluginErrorUnavailable, Message: "publish plugin version", Err: err}
	}
	for _, file := range bundle.Files {
		sum := sha256.Sum256(file.Content)
		if err := queries.CreatePluginPackageFile(ctx, db.CreatePluginPackageFileParams{
			VersionID: row.ID,
			Path:      file.Path,
			Content:   file.Content,
			SizeBytes: int64(len(file.Content)),
			Sha256:    hex.EncodeToString(sum[:]),
		}); err != nil {
			return PluginPackageSummary{}, &PluginError{Kind: PluginErrorUnavailable, Message: "store plugin package file", Err: err}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PluginPackageSummary{}, &PluginError{Kind: PluginErrorUnavailable, Message: "commit publish", Err: err}
	}
	return s.packageSummary(ctx, workspaceID, pkg)
}

// BundleDigest hashes the bundle's contents: the canonical manifest, then each
// file's path and bytes in sorted order.
//
// Deliberately not a hash of the uploaded archive. Two zips of identical files
// differ in timestamps and compression, and the question the digest answers is
// "are we looking at the same plugin", not "was this the same upload".
func BundleDigest(bundle plugincontract.Bundle) string {
	hash := sha256.New()
	hash.Write(bundle.Canonical)
	for _, file := range bundle.Files {
		fmt.Fprintf(hash, "\n%s\n%d\n", file.Path, len(file.Content))
		hash.Write(file.Content)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// resolvePublishVersion decides what version string this publish lands under.
//
// An upload keeps the manifest's version and collides loudly if it is taken —
// that collision IS the immutability guarantee, and an author who meant to
// change something needs to say so in the manifest. A local development publish
// takes `+dev.N` instead, because re-publishing an unchanged version is what
// editing a file and reloading looks like.
func resolvePublishVersion(version string, existing []db.PluginPackageVersion, devLoop bool) (string, error) {
	if !devLoop {
		return version, nil
	}
	taken := map[string]bool{}
	highest := 0
	for _, row := range existing {
		taken[row.Version] = true
		if suffix, ok := strings.CutPrefix(row.Version, version+"+dev."); ok {
			if number, err := strconv.Atoi(suffix); err == nil && number > highest {
				highest = number
			}
		}
	}
	if !taken[version] {
		return version, nil
	}
	candidate := version + "+dev." + strconv.Itoa(highest+1)
	if len(candidate) > plugincontract.MaxVersionLength {
		return "", pluginErrf(PluginErrorInvalid, "version %q leaves no room for a development suffix; shorten it below %d bytes", version, plugincontract.MaxVersionLength)
	}
	return candidate, nil
}

// lockPluginPackageKey takes the transaction-scoped lock that serializes
// publish, install and delete for one plugin. Every caller must already be in a
// transaction — the lock is released by its commit or rollback.
func lockPluginPackageKey(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, pluginKey string) error {
	if err := queries.LockPluginPackageKey(ctx, uuidString(workspaceID)+":"+pluginKey); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "lock plugin package", Err: err}
	}
	return nil
}

// upsertPackage takes `queries` rather than reading s.Queries so it runs inside
// its caller's transaction: the display name must move only if the version that
// carries it is actually stored.
func upsertPackage(ctx context.Context, queries *db.Queries, workspaceID, userID pgtype.UUID, manifest plugincontract.Manifest) (db.PluginPackage, error) {
	existing, err := queries.GetWorkspacePluginPackageByKey(ctx, db.GetWorkspacePluginPackageByKeyParams{
		WorkspaceID: workspaceID,
		PluginKey:   manifest.Key,
	})
	if err == nil {
		if existing.Name == manifest.Name {
			return existing, nil
		}
		// The display name follows the newest publish; the key does not move.
		updated, updateErr := queries.UpdatePluginPackageName(ctx, db.UpdatePluginPackageNameParams{
			ID:   existing.ID,
			Name: manifest.Name,
		})
		if updateErr != nil {
			return db.PluginPackage{}, &PluginError{Kind: PluginErrorUnavailable, Message: "update plugin package", Err: updateErr}
		}
		return updated, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.PluginPackage{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load plugin package", Err: err}
	}
	created, err := queries.CreatePluginPackage(ctx, db.CreatePluginPackageParams{
		WorkspaceID: workspaceID,
		PluginKey:   manifest.Key,
		Name:        manifest.Name,
		CreatedBy:   userID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			// Belt and braces behind the lock: a publish racing one in another
			// process that has not yet taken it still loses the index cleanly.
			return db.PluginPackage{}, pluginErrf(PluginErrorConflict, "this plugin was just published by someone else; try again")
		}
		return db.PluginPackage{}, &PluginError{Kind: PluginErrorUnavailable, Message: "create plugin package", Err: err}
	}
	return created, nil
}

// ListPackages returns everything published in this workspace with its versions.
func (s *PluginService) ListPackages(ctx context.Context, workspaceID pgtype.UUID) ([]PluginPackageSummary, error) {
	packages, err := s.Queries.ListWorkspacePluginPackages(ctx, workspaceID)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin packages", Err: err}
	}
	summaries := make([]PluginPackageSummary, 0, len(packages))
	for _, pkg := range packages {
		summary, err := s.packageSummary(ctx, workspaceID, pkg)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (s *PluginService) packageSummary(ctx context.Context, workspaceID pgtype.UUID, pkg db.PluginPackage) (PluginPackageSummary, error) {
	versions, err := s.Queries.ListPluginPackageVersions(ctx, pkg.ID)
	if err != nil {
		return PluginPackageSummary{}, &PluginError{Kind: PluginErrorUnavailable, Message: "list published versions", Err: err}
	}
	// Which version this workspace runs, if any. Read from the installation
	// rather than "the newest one": the two disagreeing is the normal state
	// after a publish, and hiding that would defeat the point.
	installedVersionID := ""
	installation, err := s.Queries.GetWorkspacePluginInstallationByKey(ctx, db.GetWorkspacePluginInstallationByKeyParams{
		WorkspaceID: workspaceID,
		PluginKey:   pkg.PluginKey,
	})
	if err == nil {
		installedVersionID = uuidString(installation.PackageVersionID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PluginPackageSummary{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load plugin installation", Err: err}
	}

	rendered := make([]PluginPackageVersionSummary, 0, len(versions))
	for _, version := range versions {
		id := uuidString(version.ID)
		rendered = append(rendered, PluginPackageVersionSummary{
			ID:          id,
			Version:     version.Version,
			Digest:      version.Digest,
			SizeBytes:   version.SizeBytes,
			PublishedAt: version.CreatedAt.Time.UTC().Format(pluginTimeFormat),
			Installed:   id != "" && id == installedVersionID,
		})
	}
	return PluginPackageSummary{
		ID:        uuidString(pkg.ID),
		PluginKey: pkg.PluginKey,
		Name:      pkg.Name,
		Versions:  rendered,
		CreatedAt: pkg.CreatedAt.Time.UTC().Format(pluginTimeFormat),
	}, nil
}

// DeletePackage removes a package with every version and file it published.
//
// Refused while any installation still runs one of those versions: deleting them
// would leave an installed panel with no code to load, and an installation is
// the administrator's decision to unwind, not the publisher's.
func (s *PluginService) DeletePackage(ctx context.Context, workspaceID pgtype.UUID, packageID string) error {
	parsed, err := util.ParseUUID(packageID)
	if err != nil {
		return pluginErrf(PluginErrorNotFound, "plugin package not found")
	}
	// Read once outside the transaction only to learn the plugin key the lock is
	// taken on. Everything the decision rests on is re-read inside it.
	pkg, err := s.Queries.GetWorkspacePluginPackage(ctx, db.GetWorkspacePluginPackageParams{
		WorkspaceID: workspaceID,
		ID:          parsed,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pluginErrf(PluginErrorNotFound, "plugin package not found")
	}
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "load plugin package", Err: err}
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "begin delete", Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.Queries.WithTx(tx)

	if err := lockPluginPackageKey(ctx, queries, workspaceID, pkg.PluginKey); err != nil {
		return err
	}
	// Counted INSIDE the lock, not before it. Counting outside would let an
	// install that started after the count commit after the delete, leaving an
	// installation whose version is gone.
	installed, err := queries.CountInstallationsOfPackageVersions(ctx, pkg.ID)
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "count installations", Err: err}
	}
	if installed > 0 {
		return pluginErrf(PluginErrorConflict, "this plugin is still installed; uninstall it before deleting the published package")
	}

	// Files first: they name a version that is about to stop existing, and there
	// are no cascades by repository policy.
	if err := queries.DeletePluginPackageFilesByPackage(ctx, pkg.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin package files", Err: err}
	}
	if err := queries.DeletePluginPackageVersionsByPackage(ctx, pkg.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin package versions", Err: err}
	}
	if err := queries.DeletePluginPackage(ctx, pkg.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin package", Err: err}
	}
	if err := tx.Commit(ctx); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "commit delete", Err: err}
	}
	return nil
}

// requireVersionStillPublished re-reads a version inside the caller's locked
// transaction.
//
// Install validates the version, its manifest and its scopes before opening a
// transaction — none of that can be done under a lock without holding it across
// work that does not need it. So the version is confirmed once more after the
// lock is held, which is the point at which a concurrent delete can no longer
// slip in between.
func requireVersionStillPublished(ctx context.Context, queries *db.Queries, workspaceID, versionID pgtype.UUID) error {
	_, err := queries.GetWorkspacePluginPackageVersion(ctx, db.GetWorkspacePluginPackageVersionParams{
		WorkspaceID: workspaceID,
		ID:          versionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pluginErrf(PluginErrorConflict, "this version was deleted while the install was being confirmed; publish or pick another version")
	}
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "re-read published plugin version", Err: err}
	}
	return nil
}

// VersionForWorkspace loads a published version and confirms it belongs to the
// workspace in the URL. Publishing is workspace-private, so a version id from
// another workspace must not be installable here.
func (s *PluginService) VersionForWorkspace(ctx context.Context, workspaceID pgtype.UUID, versionID string) (db.PluginPackageVersion, error) {
	parsed, err := util.ParseUUID(strings.TrimSpace(versionID))
	if err != nil {
		return db.PluginPackageVersion{}, pluginErrf(PluginErrorNotFound, "published plugin version not found")
	}
	version, err := s.Queries.GetWorkspacePluginPackageVersion(ctx, db.GetWorkspacePluginPackageVersionParams{
		WorkspaceID: workspaceID,
		ID:          parsed,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.PluginPackageVersion{}, pluginErrf(PluginErrorNotFound, "published plugin version not found")
	}
	if err != nil {
		return db.PluginPackageVersion{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load published plugin version", Err: err}
	}
	return version, nil
}

// PluginSurfaceScript is the code one surface runs, plus the digest of the
// version it came from.
type PluginSurfaceScript struct {
	Code    string `json:"code"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// SurfaceScript returns the script for one surface of an installation.
//
// Read from the installed version's stored files, never fetched: this is the
// method that makes "the administrator consented to this code" true rather than
// aspirational. The manifest consulted is the installation's own snapshot, so a
// surface the admin never approved cannot be named here even if a later version
// declares one.
func (s *PluginService) SurfaceScript(ctx context.Context, installation db.PluginInstallation, surfaceKey string) (PluginSurfaceScript, error) {
	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		return PluginSurfaceScript{}, &PluginError{Kind: PluginErrorInvalid, Message: "stored plugin manifest is unreadable", Err: err}
	}
	entry := ""
	for _, surface := range manifest.Contributes.Surfaces {
		if surface.Key == surfaceKey {
			entry = surface.Entry
			break
		}
	}
	if entry == "" {
		return PluginSurfaceScript{}, pluginErrf(PluginErrorNotFound, "this Plugin does not contribute a surface named %q", surfaceKey)
	}
	file, err := s.Queries.GetPluginPackageFile(ctx, db.GetPluginPackageFileParams{
		VersionID: installation.PackageVersionID,
		Path:      entry,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginSurfaceScript{}, pluginErrf(PluginErrorNotFound, "the installed version does not contain %q", entry)
	}
	if err != nil {
		return PluginSurfaceScript{}, &PluginError{Kind: PluginErrorUnavailable, Message: "read plugin surface", Err: err}
	}
	return PluginSurfaceScript{
		Code:    string(file.Content),
		Version: installation.Version,
		Digest:  file.Sha256,
	}, nil
}

// packageFile reads one file of a published version, for install-time work such
// as skill resources.
func (s *PluginService) packageFile(ctx context.Context, queries *db.Queries, versionID pgtype.UUID, entry string) ([]byte, error) {
	file, err := queries.GetPluginPackageFile(ctx, db.GetPluginPackageFileParams{
		VersionID: versionID,
		Path:      entry,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pluginErrf(PluginErrorInvalid, "the published version does not contain %q", entry)
	}
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "read plugin package file", Err: err}
	}
	return file.Content, nil
}

// readLocalFile reads a file from inside a local plugin directory.
//
// `entry` is validated by the contract package to be relative and
// traversal-free, but this is the layer that touches the filesystem and it does
// not get to assume that: the joined path is re-checked against the plugin's own
// directory after cleaning.
func (s *PluginService) readLocalFile(name, entry string) ([]byte, error) {
	if s.LocalDir == "" {
		return nil, pluginErrf(PluginErrorInvalid, "local plugin sources require MULTICA_PLUGIN_DIR")
	}
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return nil, pluginErrf(PluginErrorInvalid, "local plugin source must be a single directory name under MULTICA_PLUGIN_DIR")
	}
	root := filepath.Join(s.LocalDir, name)
	path := filepath.Clean(filepath.Join(root, entry))
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return nil, pluginErrf(PluginErrorInvalid, "plugin resource %q escapes its directory", entry)
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, pluginErrf(PluginErrorNotFound, "plugin resource %q was not found", entry)
	}
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "read plugin resource", Err: err}
	}
	return raw, nil
}
