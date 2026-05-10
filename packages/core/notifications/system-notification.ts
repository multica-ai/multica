import { createLogger } from "../logger";

const logger = createLogger("system-notification");

interface DesktopNotificationPayload {
  slug: string;
  itemId: string;
  issueKey: string;
  title: string;
  body: string;
}

interface DesktopAPI {
  showNotification?: (payload: DesktopNotificationPayload) => void;
}

function getDesktopAPI(): DesktopAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: DesktopAPI }).desktopAPI;
}

export type WebNotificationSupport =
  | "supported"
  | "permission_default"
  | "permission_denied"
  | "api_unavailable"
  | "no_window";

export function detectWebNotificationSupport(): WebNotificationSupport {
  if (typeof window === "undefined") return "no_window";
  if (typeof Notification === "undefined") return "api_unavailable";
  switch (Notification.permission) {
    case "granted":
      return "supported";
    case "denied":
      return "permission_denied";
    default:
      return "permission_default";
  }
}

export interface SystemNotificationPayload extends DesktopNotificationPayload {
  /** Path to navigate to when the user clicks the banner (web fallback only). */
  inboxPath: string;
}

/**
 * Fire a native OS notification for an inbox item, abstracting over the
 * Electron preload bridge (`window.desktopAPI`) and the browser
 * Notifications API. Returns a status string useful for diagnostics; the
 * caller is expected to have already gated on focus + the user's
 * `system_notifications` preference.
 *
 * On the desktop app the click handler routing is wired in the main process
 * (see apps/desktop/src/main/index.ts). On web we wire it here: the Notification
 * click event focuses the tab and navigates to the inbox path with the issue
 * selector pre-populated, mirroring the desktop UX as closely as the browser
 * sandbox allows.
 */
export function showSystemNotification(payload: SystemNotificationPayload): WebNotificationSupport | "delivered_desktop" {
  const desktopAPI = getDesktopAPI();
  if (desktopAPI?.showNotification) {
    desktopAPI.showNotification({
      slug: payload.slug,
      itemId: payload.itemId,
      issueKey: payload.issueKey,
      title: payload.title,
      body: payload.body,
    });
    return "delivered_desktop";
  }

  const support = detectWebNotificationSupport();
  if (support !== "supported") {
    logger.debug("skip web notification", { support, title: payload.title });
    return support;
  }

  try {
    const notification = new Notification(payload.title, {
      body: payload.body,
      tag: payload.itemId,
    });
    notification.addEventListener("click", () => {
      try {
        window.focus();
      } catch {
        // Some browsers reject window.focus() outside a user gesture; ignore.
      }
      window.location.assign(payload.inboxPath);
      notification.close();
    });
    return "supported";
  } catch (err) {
    logger.warn("web notification failed", err);
    return "api_unavailable";
  }
}

/**
 * Prompt the browser for notification permission. Must be invoked from a
 * user gesture (click, keypress) or the request is silently denied in many
 * browsers. Returns the resulting permission state, or "unsupported" if the
 * Notifications API is missing entirely.
 */
export async function requestWebNotificationPermission(): Promise<
  "granted" | "denied" | "default" | "unsupported"
> {
  if (typeof window === "undefined" || typeof Notification === "undefined") {
    return "unsupported";
  }
  if (Notification.permission === "granted" || Notification.permission === "denied") {
    return Notification.permission;
  }
  try {
    const result = await Notification.requestPermission();
    return result;
  } catch (err) {
    logger.warn("requestPermission failed", err);
    return "denied";
  }
}

/**
 * True when this build is the Electron desktop app — the preload script
 * injects `window.desktopAPI`. Used by the settings UI to hide the
 * browser-permission affordance, since the main process owns notifications
 * on desktop.
 */
export function isDesktopApp(): boolean {
  return Boolean(getDesktopAPI());
}
