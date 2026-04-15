import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// Legacy paths that should redirect to locale-prefixed versions
const LEGACY_PATHS = [
  "/imprint",
  "/privacy-policy",
  "/terms",
  "/cookies",
] as const;

// Supported locales (must match locales in features/landing/i18n/types.ts)
const LOCALES = ["en", "de", "zh"] as const;
type Locale = (typeof LOCALES)[number];

function detectLocale(request: NextRequest): Locale {
  // 1. Check cookie first
  const cookie = request.cookies.get("multica-locale")?.value;
  if (cookie && LOCALES.includes(cookie as Locale)) {
    return cookie as Locale;
  }

  // 2. Check Accept-Language header
  const acceptLang = request.headers.get("accept-language") ?? "";
  if (acceptLang.includes("zh")) return "zh";
  if (acceptLang.includes("de")) return "de";

  return "en";
}

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  // Only handle legacy paths (no locale prefix)
  const isLegacyPath = LEGACY_PATHS.some((p) => pathname === p || pathname.startsWith(p + "/"));

  if (!isLegacyPath) {
    return NextResponse.next();
  }

  // Detect user's preferred locale
  const locale = detectLocale(request);

  // Build the redirect URL
  const redirectUrl = request.nextUrl.clone();
  redirectUrl.pathname = `/${locale}${pathname}`;

  return NextResponse.redirect(redirectUrl, 308);
}

export const config = {
  matcher: [
    /*
     * Match all request paths except:
     * - api routes
     * - _next/static (static files)
     * - _next/image (image optimization)
     * - favicon.ico, sitemap.xml, robots.xml
     * - files with extensions
     */
    "/((?!api|_next/static|_next/image|favicon.ico|sitemap.xml|robots.xml|.*\\..*).*)",
  ],
};
