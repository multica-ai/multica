import { NextResponse, type NextRequest } from "next/server";

const runtimeUrl = (process.env.CEREBRO_APPS_RUNTIME_URL ?? "http://127.0.0.1:4310").replace(/\/$/, "");

async function proxy(request: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  const { path } = await context.params;
  const target = `${runtimeUrl}/${path.map(encodeURIComponent).join("/")}${request.nextUrl.pathname.endsWith("/") ? "/" : ""}${request.nextUrl.search}`;
  const body = request.method === "GET" || request.method === "HEAD" ? undefined : await request.arrayBuffer();
  const response = await fetch(target, { method: request.method, headers: { "content-type": request.headers.get("content-type") ?? "application/octet-stream" }, body, cache: "no-store" });
  const headers = new Headers();
  for (const name of ["cache-control", "content-type", "content-security-policy"]) {
    const value = response.headers.get(name);
    if (value) headers.set(name, value);
  }
  return new NextResponse(response.body, { status: response.status, headers });
}

export const GET = proxy;
export const POST = proxy;
