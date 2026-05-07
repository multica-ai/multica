// Browser-only entry: anything that touches `document` / `navigator` /
// `window` lives here, so accidental imports from RSC, Edge middleware, or
// nodejs proxy.ts crash at the import site instead of at first call.
//
// `LOCALE_COOKIE` (a plain string constant) is also exported from the
// server-safe `@multica/core/i18n` entry — proxy.ts needs it to read the
// cookie from a NextRequest. Only the adapter factory is browser-restricted.
export { createBrowserCookieLocaleAdapter } from "./browser-cookie-adapter";
