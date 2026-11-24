-- Rolling back returns installations to naming a source URL. Published versions
-- are dropped with the tables that hold them; there is nowhere else to put the
-- bytes, and an installation pointing at a version that no longer exists would
-- render a panel that cannot load.
DELETE FROM skill WHERE plugin_installation_id IS NOT NULL;
DELETE FROM plugin_invocation;
DELETE FROM plugin_secret;
DELETE FROM plugin_storage;
DELETE FROM plugin_installation;

ALTER TABLE plugin_installation DROP COLUMN IF EXISTS package_version_id;
ALTER TABLE plugin_installation
    ADD COLUMN source_url TEXT NOT NULL DEFAULT '' CHECK (char_length(source_url) BETWEEN 0 AND 2048);

DROP TABLE IF EXISTS plugin_package_file;
DROP TABLE IF EXISTS plugin_package_version;
DROP TABLE IF EXISTS plugin_package;
