import { createHash, createHmac, timingSafeEqual } from "node:crypto";

export function signServiceRequest(secret, method, path, body = Buffer.alloc(0), now = new Date()) {
  const timestamp = now.toISOString();
  return { timestamp, signature: signatureForTimestamp(secret, method, path, body, timestamp) };
}

export function verifyServiceRequest(secret, method, path, body, signed, now = new Date()) {
  const timestamp = signed?.timestamp ?? "";
  const signedAt = Date.parse(timestamp);
  if (!Number.isFinite(signedAt) || Math.abs(now.getTime() - signedAt) > 120_000) return false;
  const expected = signatureForTimestamp(secret, method, path, body, timestamp);
  return safeEqual(expected, signed.signature ?? "");
}

function signatureForTimestamp(secret, method, path, body, timestamp) {
  const bodyHash = createHash("sha256").update(body).digest("hex");
  const signature = createHmac("sha256", secret).update(`${method}\n${path}\n${bodyHash}\n${timestamp}`).digest("hex");
  return `sha256=${signature}`;
}

// The bundle token is a durable capability: it authorizes exactly one worker to
// download its own (immutable, hash-verified) app bundle. A worker re-downloads
// the bundle on every container start, and its Sliplane env is immutable, so the
// token must stay valid for the worker's entire lifetime. It therefore carries no
// expiry — an expiring token baked into a long-lived, restart-on-demand service
// crashes every restart past the TTL. Short-lived, per-request user capabilities
// use mintInvocationGrant (which keeps its expiry) instead.
export function mintBundleToken(secret, appId, version) {
  const payload = Buffer.from(JSON.stringify({ app_id: appId, version })).toString("base64url");
  const signature = createHmac("sha256", secret).update(payload).digest("base64url");
  return `${payload}.${signature}`;
}

function safeEqual(left, right) {
  const a = Buffer.from(String(left));
  const b = Buffer.from(String(right));
  return a.length === b.length && timingSafeEqual(a, b);
}
