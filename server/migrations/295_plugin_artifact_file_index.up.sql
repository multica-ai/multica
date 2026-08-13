CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_artifact_file_release_path
    ON plugin_artifact_file (release_id, path);
