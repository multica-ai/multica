import createMiddleware from "next-intl/middleware";
import { routing } from "./i18n/routing";

export default createMiddleware(routing);

export const config = {
  // Apply locale routing only to auth/dashboard pages.
  // Excludes: root `/` (landing), /about, /changelog, /homepage, /auth/*, /api/*, _next, static assets.
  matcher: [
    "/((?!_next|api|auth|about|changelog|homepage|favicon\\.ico|favicon\\.svg|robots\\.txt|sitemap\\.xml).+)",
  ],
};
