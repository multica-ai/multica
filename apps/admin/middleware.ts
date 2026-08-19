import { NextResponse, type NextRequest } from "next/server";
import { isUnauthenticatedExposureBlocked } from "@/lib/access-guard";

// Kill switch for accidental production exposure — see lib/access-guard.ts
// for the full rationale. Runs before every request (including route
// handlers, which have direct Postgres/LiteLLM access and no auth of
// their own).
export function middleware(_request: NextRequest) {
  if (isUnauthenticatedExposureBlocked(process.env)) {
    return new NextResponse(
      "Multica Admin is disabled in production until access control is " +
        "added. Set ADMIN_ALLOW_UNSAFE_NO_AUTH=true to override.",
      { status: 503 },
    );
  }
  return NextResponse.next();
}

export const config = {
  matcher: "/:path*",
};
