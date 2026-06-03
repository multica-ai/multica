import {
  buildAppleAppSiteAssociation,
  hasAppleAppIds,
} from "../../../lib/mobile-app-association";

export const dynamic = "force-dynamic";

export function GET() {
  if (!hasAppleAppIds()) {
    return new Response("Missing Multica iOS associated-domain configuration.", {
      status: 404,
    });
  }

  return Response.json(buildAppleAppSiteAssociation());
}
