import { NextResponse } from "next/server";
import { litellmConfigured } from "@/lib/litellm";

// GET /api/litellm/health — lets the client show a one-time banner
// ("LiteLLM not configured — cost/team data unavailable") instead of the
// detail panel silently rendering empty LiteLLM sections with no explanation.
export async function GET() {
  return NextResponse.json({ configured: litellmConfigured() });
}
