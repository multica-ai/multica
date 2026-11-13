import { proxyUpload } from "@/platform/upload-proxy";

export const runtime = "nodejs";

export async function POST(request: Request): Promise<Response> {
  return proxyUpload(request);
}

export async function OPTIONS(request: Request): Promise<Response> {
  return proxyUpload(request);
}
