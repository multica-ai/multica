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
import { applyAppBadge, type BadgingNavigator } from "./sw-badge";

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
    data: { url, issueId: payload.issueId, type: payload.type },
  };
  if (payload.tag) {
    (opts as NotificationOptions & { renotify?: boolean }).renotify = true;
  }
  const badge = applyBadgeFromSelf(payload.unreadCount);
  event.waitUntil(
    badge
      ? Promise.all([self.registration.showNotification(title, opts), badge])
      : self.registration.showNotification(title, opts),
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
