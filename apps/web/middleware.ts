import createMiddleware from "next-intl/middleware";
import { routing } from "./i18n/routing";

export default createMiddleware(routing);

export const config = {
  // Match all pathnames except static files and Next.js internals
  matcher: [
    "/((?!api|_next/static|_next/image|favicon.ico|favicon.svg|robots.txt|sitemap.xml).*)",
  ],
};
