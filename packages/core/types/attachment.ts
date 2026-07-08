export interface Attachment {
  id: string;
  workspace_id: string;
  issue_id: string | null;
  comment_id: string | null;
  chat_session_id: string | null;
  chat_message_id: string | null;
  uploader_type: string;
  uploader_id: string;
  filename: string;
  url: string;
  download_url: string;
  /**
   * Durable URL the client persists into markdown bodies.
   *
   * The server (`buildMarkdownURL` in server/internal/handler/file.go)
   * computes this per deployment policy:
   *   - public CDN path when storage URL is itself absolute and unsigned;
   *   - otherwise `<MULTICA_PUBLIC_URL>/api/attachments/<id>/download`,
   *     which the server self-resigns / proxies on every request.
   *
   * Distinct from `url` (raw storage URL — may be private / site-relative)
   * and `download_url` (this-response click-time URL — may be a short-lived
   * CloudFront / S3 signed URL with a TTL). `markdown_url` is contracted
   * to be safe to embed in markdown bodies that outlive the current
   * session and to load as a native browser resource fetch on every
   * supported client (web / desktop / mobile webview). MUL-3192.
   *
   * Empty when the response was produced by a server old enough to
   * predate this field, or by an upload path that did not produce a
   * persisted attachment row (e.g. the no-workspace avatar branch).
   * Frontend callers that need to embed a URL into markdown should use
   * the helper in `useFileUpload` rather than reading this field
   * directly so the legacy fallbacks (download path / `att.url`) stay
   * centralized.
   */
  markdown_url: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
}
