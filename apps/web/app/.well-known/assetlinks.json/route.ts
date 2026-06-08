import {
  buildAndroidAssetLinks,
  hasAndroidAssetLinks,
} from "../../../lib/mobile-app-association";

export const dynamic = "force-dynamic";

export function GET() {
  if (!hasAndroidAssetLinks()) {
    return new Response("Missing Multica Android App Links configuration.", {
      status: 404,
    });
  }

  return Response.json(buildAndroidAssetLinks());
}
