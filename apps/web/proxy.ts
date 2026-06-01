import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// Runtime-proxy routes: these rewrites are evaluated on every request so
// REMOTE_API_URL can be overridden at runtime (e.g. via Helm extraEnv)
// without rebuilding the image. Static rewrites (/docs) stay in next.config.ts.
const proxyPrefixes = ["/api/", "/auth/", "/uploads/"];
const proxyExact = ["/ws"];

export function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;

  const shouldProxy =
    proxyPrefixes.some((p) => pathname.startsWith(p)) ||
    proxyExact.includes(pathname);

  if (!shouldProxy) {
    return NextResponse.next();
  }

  // Read backend URL from runtime env (falls back to the build-time default).
  const remoteApiUrl =
    process.env.REMOTE_API_URL || "http://localhost:8080";

  const url = new URL(pathname + request.nextUrl.search, remoteApiUrl);

  return NextResponse.rewrite(url);
}
