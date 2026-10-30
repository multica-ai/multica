-- Platform-hosted plugin artifacts.
--
-- Until now a plugin was a URL: the manifest was fetched once and frozen as the
-- consented snapshot, but the surface script was loaded from the author's server
-- every time a panel opened. So the administrator consented to a manifest while
-- the browser ran whatever code that host served that day, the author's uptime
-- became our uptime, and every panel open told the author who was reading which
-- issue.
--
-- What replaces it: the author uploads an artifact bundle, Multica stores it,
-- and an installation is bound to one immutable version. Nothing about a
-- published version is ever updated in place — a new publish is a new row, and
-- an installed workspace keeps running the version it consented to until an
-- administrator upgrades it.
--
-- Hook endpoints and MCP servers stay on the author's own infrastructure. Only
-- the frontend artifact moved.

-- One publishable plugin identity per workspace. Publishing is workspace-private
-- for now; a public directory needs review, reporting and takedown, which is a
-- separate decision from where the bytes live.
CREATE TABLE plugin_package (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    plugin_key TEXT NOT NULL CHECK (char_length(plugin_key) BETWEEN 3 AND 255),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One published version. Immutable by construction: no statement in the
-- application updates this row, and republishing an existing version is a
-- conflict rather than an overwrite. That is the whole point — "what the admin
-- approved" and "what the browser runs" have to name the same bytes.
--
-- workspace_id is denormalized so every read can be scoped without joining
-- through plugin_package; relationships stay application-owned by repository
-- policy, so there is no foreign key to join through cheaply anyway.
CREATE TABLE plugin_package_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    package_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    version TEXT NOT NULL CHECK (char_length(version) BETWEEN 1 AND 64),
    manifest JSONB NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    -- sha256 of the uploaded bundle, hex. Shown to the publisher so two people
    -- can confirm they are looking at the same artifact.
    digest TEXT NOT NULL CHECK (char_length(digest) = 64),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    published_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The files a version ships: the surface scripts, the skill text, the icon.
--
-- Stored in Postgres rather than object storage. A bundle is capped at a few
-- hundred kilobytes by the service, and this way an installed plugin cannot be
-- half-available: the manifest snapshot and the code it names commit together
-- and are restored together. Object storage would reintroduce, against our own
-- bucket, exactly the availability split this change exists to remove.
CREATE TABLE plugin_package_file (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL,
    path TEXT NOT NULL CHECK (char_length(path) BETWEEN 1 AND 1024),
    content BYTEA NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    sha256 TEXT NOT NULL CHECK (char_length(sha256) = 64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every existing installation names a source URL, which is no longer a concept.
-- There is nothing to migrate them to: the code they would run was never
-- published here, so binding them to a version would be an invention. plugins_v1
-- has never left its feature flag and holds no production data, so they are
-- removed — the same call migration 344 made for the same reason.
DELETE FROM skill WHERE plugin_installation_id IS NOT NULL;
DELETE FROM plugin_invocation;
DELETE FROM plugin_secret;
DELETE FROM plugin_storage;
DELETE FROM plugin_installation;

ALTER TABLE plugin_installation DROP COLUMN IF EXISTS source_url;
ALTER TABLE plugin_installation ADD COLUMN package_version_id UUID NOT NULL;
