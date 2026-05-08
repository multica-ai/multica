// PWA service worker compiled by @serwist/next from app/sw.ts → public/sw.js.
//
// This file runs in a ServiceWorker scope (no DOM, no window). It is
// excluded from the main apps/web/tsconfig.json and typechecked via
// tsconfig.sw.json which loads the WebWorker lib instead of DOM. Doing
// it that way (vs. inline `/// <reference lib="webworker"/>`) avoids
// leaking Worker globals into the rest of the app's type universe and
// avoids no-default-lib's project-wide side effects.
import { defaultCache } from "@serwist/next/worker";
import type { PrecacheEntry, SerwistGlobalConfig } from "serwist";
import { Serwist } from "serwist";
import { applyAppBadge, resolveBadgeCount, type BadgingNavigator } from "./sw-badge";
import {
  newDraftToken,
  saveShareDraft,
  type ShareDraft,
  type ShareDraftFile,
} from "./cerebro-share-draft-idb";

declare global {
  interface WorkerGlobalScope extends SerwistGlobalConfig {
    __SW_MANIFEST: (PrecacheEntry | string)[] | undefined;
  }
}

declare const self: ServiceWorkerGlobalScope;

const serwist = new Serwist({
  precacheEntries: self.__SW_MANIFEST,
  skipWaiting: true,
  clientsClaim: true,
  navigationPreload: true,
  runtimeCaching: defaultCache,
});

serwist.addEventListeners();

// --- Web Push ---------------------------------------------------------------
// Handlers for incoming push messages and notification clicks. The payload
// shape is decided server-side in service.PushService.Payload — keep field
// names in sync.

interface PushPayload {
  title: string;
  body?: string;
  url?: string;
  tag?: string;
  issueId?: string;
  type?: string;
  // Total unread inbox items for the recipient across every workspace.
  // Drives the OS-level PWA app-icon badge via the Badging API. Always set
  // by the server on real pushes; absent only on fallback/empty payloads.
  unreadCount?: number;
  // Server-side echo of preferences.notifications.channels.mobile.sound=off.
  // When true the SW shows the notification without sound/vibration.
  silent?: boolean;
}

// Badging API is available on iOS 16.4+ Safari (installed PWA), Android
// Chrome and desktop Chrome — on other browsers the methods are absent and
// the service worker must not crash. Read off the worker's `self.navigator`,
// then defer to the shared sw-badge helper for the actual decision tree
// (reused by the test suite and by the page-side postMessage bridge).
function applyBadgeFromSelf(count: number | undefined): Promise<void> | undefined {
  const nav = (self as unknown as { navigator?: BadgingNavigator }).navigator;
  return applyAppBadge(nav, count);
}

self.addEventListener("push", (event) => {
  // No payload at all means the browser woke us up with no data attached;
  // show a generic placeholder rather than nothing so the user knows
  // something happened.
  let payload: PushPayload = { title: "Multica", body: "You have a new notification" };
  if (event.data) {
    try {
      payload = event.data.json() as PushPayload;
    } catch {
      const text = event.data.text();
      if (text) {
        payload = { title: "Multica", body: text };
      }
    }
  }

  const url = payload.url ?? "/";
  const title = payload.title || "Multica";

  // renotify=true plays the sound/vibrate even when a same-tag notification
  // is already on screen, so a follow-up comment still grabs attention on
  // iOS. Cast through NotificationOptions because lib.webworker.d.ts in this
  // TS version doesn't include renotify yet, even though browsers support it.
  const opts: NotificationOptions = {
    body: payload.body,
    // tag collapses bursts on the same issue into one visible notification.
    tag: payload.tag,
    icon: "/icons/icon-192.png",
    badge: "/icons/icon-192.png",
    silent: payload.silent === true,
    data: { url, issueId: payload.issueId, type: payload.type },
  };
  if (payload.tag) {
    (opts as NotificationOptions & { renotify?: boolean }).renotify = true;
  }

  // Always run the badge update inside waitUntil so the SW stays alive long
  // enough to call setAppBadge. If we let the push handler return without
  // touching the badge, iOS WebKit auto-increments by 1 — the source of the
  // "badge keeps adding more" bug.
  event.waitUntil(
    Promise.all([
      self.registration.showNotification(title, opts),
      (async () => {
        const count = await resolveBadgeCount(payload.unreadCount);
        const promise = applyBadgeFromSelf(count);
        if (promise) await promise;
      })(),
    ]),
  );
});

// Allow the page to drive the OS badge directly — used after the user reads
// or archives inbox items in-app, where no push fires but the badge needs to
// reflect the new count immediately.
self.addEventListener("message", (event) => {
  const data = event.data as { type?: string; count?: number } | undefined;
  if (!data || data.type !== "set-app-badge") return;
  const promise = applyBadgeFromSelf(data.count);
  if (promise && "waitUntil" in event) {
    (event as ExtendableMessageEvent).waitUntil(promise);
  }
});

// --- Web Share Target --------------------------------------------------------
// The OS share-sheet POSTs multipart form-data to /share/issue (the action
// declared in manifest.webmanifest). Intercepting it here lets us stash the
// payload locally and hand control off to a normal GET page — the canonical
// Web Share Target pattern. Without the SW the POST would hit the Next.js
// server, which has no way to ferry binary files into the user's browser
// session post-auth.
//
// Files larger than this cap are dropped from the draft; the page surfaces a
// banner listing what was skipped. The cap matches the acceptance criterion
// in JEH-734 — graceful handling, not a crash. Server-side attachment limits
// are higher (100MB), so this is purely a share-flow concession to keep IDB
// quota usage modest on phones.
const SHARE_TARGET_MAX_FILE_BYTES = 5 * 1024 * 1024;

async function handleShareTargetPOST(request: Request): Promise<Response> {
  const token = newDraftToken();
  const redirectURL = new URL("/share/issue", self.location.origin);
  redirectURL.searchParams.set("draft", token);

  try {
    const form = await request.formData();
    const title = stringFromForm(form, "title");
    const text = stringFromForm(form, "text");
    const url = stringFromForm(form, "url");

    const files: ShareDraftFile[] = [];
    const oversized: string[] = [];
    for (const value of form.getAll("files")) {
      if (!(value instanceof File)) continue;
      if (value.size === 0) continue;
      if (value.size > SHARE_TARGET_MAX_FILE_BYTES) {
        oversized.push(value.name || "untitled");
        continue;
      }
      files.push({
        name: value.name || "untitled",
        type: value.type || "application/octet-stream",
        // Reading into a Blob detaches us from the streaming Request; IDB
        // stores Blobs natively and the page reconstructs File objects from
        // them when re-uploading via the regular attachments endpoint.
        blob: await value.arrayBuffer().then((buf) => new Blob([buf], { type: value.type })),
      });
    }

    const draft: ShareDraft = {
      token,
      title,
      text,
      url,
      files,
      createdAt: Date.now(),
    };
    await saveShareDraft(draft);

    if (oversized.length > 0) {
      redirectURL.searchParams.set("oversized", oversized.join("|"));
    }
  } catch {
    // Persisting failed — most likely IDB quota or a malformed multipart
    // body. Surface as an error param so the page shows a friendly message
    // instead of a blank screen.
    redirectURL.searchParams.set("error", "save_failed");
  }
  return Response.redirect(redirectURL.toString(), 303);
}

function stringFromForm(form: FormData, key: string): string {
  const v = form.get(key);
  return typeof v === "string" ? v : "";
}

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "POST") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname !== "/share/issue") return;
  event.respondWith(handleShareTargetPOST(request));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const data = (event.notification.data ?? {}) as { url?: string };
  const targetUrl = data.url || "/";

  event.waitUntil(
    (async () => {
      // Focus an existing client if the app is already open; otherwise launch.
      const all = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      const target = new URL(targetUrl, self.location.origin);
      for (const client of all) {
        const clientUrl = new URL(client.url);
        if (clientUrl.origin === target.origin) {
          await client.focus();
          if ("navigate" in client) {
            await client.navigate(target.href).catch(() => undefined);
          }
          return;
        }
      }
      await self.clients.openWindow(target.href);
    })(),
  );
});
