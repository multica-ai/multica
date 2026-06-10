/**
 * Attachment download URLs and workspace context.
 *
 * The API returns `download_url` as a server-relative path
 * (`/api/attachments/{id}/download`). That route sits behind the
 * workspace-membership middleware, which resolves the target workspace from
 * the `X-Workspace-Slug` header the app's API client normally attaches.
 *
 * A plain top-level browser navigation — `window.open(...)`, an `<a href>`
 * click, or a credentialed `fetch` for inline rendering — cannot set that
 * header. The request then arrives with no workspace context and the
 * middleware rejects it with 400 "workspace_id or workspace_slug is required"
 * (the broken "can't download agent files" symptom).
 *
 * Appending the workspace id as a query param makes the URL self-sufficient
 * for plain navigation; the middleware still validates the caller's
 * membership of that workspace, so this widens nothing. Absolute URLs (signed
 * CloudFront/CDN links) already carry their own auth and are returned
 * untouched.
 */
export function attachmentDownloadHref(
  downloadUrl: string | undefined,
  workspaceId: string,
): string {
  if (!downloadUrl) return downloadUrl ?? "";
  // Absolute URL (e.g. signed CDN link) — already self-authenticating.
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(downloadUrl)) return downloadUrl;
  if (!workspaceId) return downloadUrl;
  // Don't double-append if a workspace identifier is already present.
  if (/[?&]workspace_(id|slug)=/.test(downloadUrl)) return downloadUrl;
  const sep = downloadUrl.includes("?") ? "&" : "?";
  return `${downloadUrl}${sep}workspace_id=${encodeURIComponent(workspaceId)}`;
}
