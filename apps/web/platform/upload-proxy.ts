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

function forwardedHeaders(source: Headers, request: boolean): Headers {
  const headers = new Headers(source);
  for (const name of HOP_BY_HOP_HEADERS) headers.delete(name);
  // The outgoing fetch selects chunked transfer encoding for the stream. A
  // stale browser-supplied length would make intermediaries mis-frame it.
  if (request) headers.delete("content-length");
  return headers;
}

function uploadOrigin(env: RuntimeEnv): string | undefined {
  return env.NODE_ENV === "development"
    ? resolveDevRemoteApiUrl(env)
    : resolveRemoteApiUrl(env);
}

/** Forward the browser multipart body without materializing it in Next.js. */
export async function proxyUpload(
  request: Request,
  env: RuntimeEnv = process.env,
  fetchImpl: FetchLike = fetch,
): Promise<Response> {
  const origin = uploadOrigin(env);
  if (!origin) {
    return Response.json(
      { error: "file upload backend is not configured" },
      { status: 503 },
    );
  }

  const incomingURL = new URL(request.url);
  const upstreamURL = new URL(origin);
  upstreamURL.pathname = `${upstreamURL.pathname.replace(/\/+$/, "")}/api/upload-file`;
  upstreamURL.search = incomingURL.search;

  try {
    // Node's fetch requires duplex for streaming request bodies. It is an
    // Undici extension that is intentionally kept local to this boundary.
    const init: RequestInit & { duplex: "half" } = {
      method: "POST",
      headers: forwardedHeaders(request.headers, true),
      body: request.body,
      redirect: "manual",
      signal: request.signal,
      duplex: "half",
    };
    const upstream = await fetchImpl(upstreamURL, init);
    return new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: forwardedHeaders(upstream.headers, false),
    });
  } catch (error) {
    console.error("upload proxy request failed", {
      error: error instanceof Error ? error.message : String(error),
    });
    return Response.json({ error: "file upload backend unavailable" }, { status: 502 });
  }
}
