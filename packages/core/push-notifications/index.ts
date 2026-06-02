/**
 * Browser Push Notifications
 *
 * Frontend module that wires the Web Push API to the Wallts notification
 * pipeline:
 *
 *   1. A registered Service Worker (`/sw.js`) receives push events and
 *      renders native OS notifications.
 *   2. The user opts in from Settings → Notifications → Browser Push; the
 *      resulting `PushSubscription` is persisted to the server so the backend
 *      can deliver pushes when an event fires (mention, assignment, agent
 *      run, etc.).
 *   3. Clicking a notification navigates the focused tab to the relevant
 *      issue; if no tab is open, a new window is opened.
 *
 * Boundaries:
 *   - `core` is platform-agnostic; this module only touches the Push API
 *     inside browser-only guards (`typeof window !== "undefined"`).
 *   - Service worker registration lives in the web app
 *     (`apps/web/components/push-notification-registrar.tsx`); the data
 *     hooks and API calls live here so the desktop and any future web
 *     client can reuse them.
 */

export * from "./queries";
export * from "./mutations";
export * from "./subscription";
export * from "./types";
