import {
  resolveDevRemoteApiUrl,
  resolveRemoteApiUrl,
} from "../config/runtime-urls";

type RuntimeEnv = Record<string, string | undefined>;
type FetchLike = (input: string | URL | Request, init?: RequestInit) => Promise<Response>;

const HOP_BY_HOP_HEADERS = [
  "connection",
  "host",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "proxy-connection",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
];

function forwardedHeaders(source: Headers): Headers {
  const headers = new Headers(source);
  for (const name of source.get("connection")?.split(",") ?? []) {
    if (name.trim()) headers.delete(name.trim());
  }
  for (const name of HOP_BY_HOP_HEADERS) headers.delete(name);
  return headers;
}

/** Forward multipart bytes without buffering or parsing them in Next.js. */
export async function proxyUpload(
  request: Request,
  env: RuntimeEnv = process.env,
  fetchImpl: FetchLike = fetch,
): Promise<Response> {
  const origin = env.NODE_ENV === "development"
    ? resolveDevRemoteApiUrl(env)
    : resolveRemoteApiUrl(env);
  if (!origin) {
    return Response.json({ error: "file upload backend is not configured" }, { status: 503 });
  }

  const incomingURL = new URL(request.url);
  const upstreamURL = new URL(origin);
  upstreamURL.pathname = `${upstreamURL.pathname.replace(/\/+$/, "")}/api/upload-file`;
  upstreamURL.search = incomingURL.search;
  const headers = forwardedHeaders(request.headers);
  // The outgoing streaming request uses its own transfer framing.
  headers.delete("content-length");
  // Next's HTTP server handles 100-continue; Node fetch rejects Expect.
  headers.delete("expect");
  headers.set("x-forwarded-host", request.headers.get("host") ?? incomingURL.host);
  headers.set("accept-encoding", "identity");

  try {
    // Node fetch requires duplex for a streaming request body.
    const init: RequestInit & { duplex: "half" } = {
      method: request.method,
      headers,
      body: request.body,
      redirect: "manual",
      signal: request.signal,
      duplex: "half",
    };
    const upstream = await fetchImpl(upstreamURL, init);
    const responseHeaders = forwardedHeaders(upstream.headers);
    // Fetch decodes compressed responses, so their wire encoding and length
    // cannot describe the response stream returned to the browser.
    responseHeaders.delete("content-encoding");
    responseHeaders.delete("content-length");
    return new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: responseHeaders,
    });
  } catch (error) {
    if (request.signal.aborted) throw error;
    console.error("upload proxy request failed", {
      error: error instanceof Error ? error.message : String(error),
    });
    return Response.json({ error: "file upload backend unavailable" }, { status: 502 });
  }
}
