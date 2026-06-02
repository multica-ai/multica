/**
 * Multica Service Worker
 *
 * Handles Web Push events delivered by the browser push service
 * (Chrome/Firefox/Edge/Safari) and renders native OS notifications.
 *
 * Message format (sent by the Go backend):
 *
 *   {
 *     "title": "Mentioned in AIH-43",
 *     "body":  "@you — can you review this?",
 *     "icon":  "/favicon.svg",
 *     "data": {
 *       "type":      "mention" | "assignment" | "agent_run" | "build_failure",
 *       "issue_id":  "50c8c349-...",
 *       "comment_id": "..."        // optional, for mentions
 *     }
 *   }
 *
 * The `data.type` field drives the notification click URL.
 *
 * Lifecycle:
 *   - Registered once at app boot via `navigator.serviceWorker.register('/sw.js')`.
 *   - Only active in web app; desktop uses Electron's native notification API.
 */

// ---------------------------------------------------------------------------
// Push event handler
// ---------------------------------------------------------------------------

self.addEventListener("push", (event) => {
  if (!event.data) return;

  let payload;
  try {
    payload = event.data.json();
  } catch {
    // Treat unparseable pushes as a plain body-only notification.
    payload = { title: "Multica", body: event.data.text() };
  }

  const { title, body, icon, data } = payload;

  const notificationTitle = title || "Multica";

  /** @type {NotificationOptions} */
  const notificationOptions = {
    body: body || "",
    icon: icon || "/favicon.svg",
    badge: "/favicon.svg",
    tag: data?.type && data?.issue_id
      ? `${data.type}:${data.issue_id}`
      : undefined,
    data: data || {},
    // Merge in notification behaviour hints from the payload when present.
    ...(payload.requireInteraction != null
      ? { requireInteraction: payload.requireInteraction }
      : {}),
    ...(payload.actions != null ? { actions: payload.actions } : {}),
  };

  event.waitUntil(
    self.registration.showNotification(notificationTitle, notificationOptions),
  );
});

// ---------------------------------------------------------------------------
// Notification click → navigate to the relevant issue
// ---------------------------------------------------------------------------

self.addEventListener("notificationclick", (event) => {
  event.notification.close();

  const { data } = event.notification;
  const url = buildNavigationUrl(data);

  if (!url) return;

  event.waitUntil(
    (async () => {
      // Prefer focusing an existing window that is already on this workspace
      // (or any workspace). Opening a second tab is disruptive and the
      // notification context is workspace-scoped, so a single focused tab
      // is the right UX.
      const allClients = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });

      // Try to find an existing client on the same origin.
      const targetClient = allClients.find((client) => {
        try {
          const clientUrl = new URL(client.url);
          return clientUrl.origin === self.location.origin;
        } catch {
          return false;
        }
      });

      if (targetClient) {
        // Focus the existing tab. The client-side code handles URL-based
        // navigation when it receives the `navigate` message.
        await targetClient.focus();
        targetClient.postMessage({ type: "navigate", url });
      } else {
        // No existing tab — open a new window.
        await self.clients.openWindow(url);
      }
    })(),
  );
});

// ---------------------------------------------------------------------------
// Activate — claim existing clients so the SW takes effect immediately
// ---------------------------------------------------------------------------

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Derive a full URL from the push notification's data payload.
 *
 * The backend includes `data.issue_id` (and optionally `data.comment_id`)
 * on every notification type. The URL targets the issue detail page; if a
 * comment_id is present a `#comment-<id>` fragment is appended so the
 * detail view can scroll to the exact comment.
 *
 * @param {Record<string, unknown> | undefined} data
 * @returns {string | null}
 */
function buildNavigationUrl(data) {
  if (!data?.issue_id) return null;

  const workspaceSlug = data.workspace_slug;
  const issueId = data.issue_id;

  // If the backend supplies a workspace slug, build a workspace-scoped URL;
  // otherwise fall back to a global issue route that the web app resolves.
  let url;
  if (workspaceSlug) {
    url = `/${workspaceSlug}/issues/${issueId}`;
  } else {
    url = `/issues/${issueId}`;
  }

  if (data.comment_id) {
    url += `#comment-${data.comment_id}`;
  }

  return url;
}
