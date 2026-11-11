-- name: ListThemeStoreDownloads :many
SELECT theme_id, downloads FROM theme_store_download;

-- name: AddThemeStoreDownload :exec
INSERT INTO theme_store_download (theme_id, downloads)
VALUES ($1, 1)
ON CONFLICT (theme_id) DO UPDATE
SET downloads = theme_store_download.downloads + 1, updated_at = now();
