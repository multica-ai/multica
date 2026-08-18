import { proxyUpload } from "@/platform/upload-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: Request): Promise<Response> {
  return proxyUpload(request);
}
