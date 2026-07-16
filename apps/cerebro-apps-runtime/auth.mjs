import { createHash, createHmac, timingSafeEqual } from "node:crypto";

export function signServiceRequest(secret, method, path, body = Buffer.alloc(0), now = new Date()) {
  const timestamp = now.toISOString();
  const bodyHash = createHash("sha256").update(body).digest("hex");
  const signature = createHmac("sha256", secret).update(`${method}\n${path}\n${bodyHash}\n${timestamp}`).digest("hex");
  return { timestamp, signature: `sha256=${signature}` };
}

export function verifyServiceRequest(secret, method, path, body, signed, now = new Date()) {
  const signedAt = Date.parse(signed?.timestamp ?? "");
  if (!Number.isFinite(signedAt) || Math.abs(now.getTime() - signedAt) > 120_000) return false;
  const expected = signServiceRequest(secret, method, path, body, new Date(signedAt));
  return safeEqual(expected.signature, signed.signature ?? "");
}

export function mintBundleToken(secret, appId, version, now = new Date(), ttlMs = 30 * 60_000) {
  const payload = Buffer.from(JSON.stringify({ app_id: appId, version, exp: Math.floor((now.getTime() + ttlMs) / 1000) })).toString("base64url");
  const signature = createHmac("sha256", secret).update(payload).digest("base64url");
  return `${payload}.${signature}`;
}

function safeEqual(left, right) {
  const a = Buffer.from(String(left));
  const b = Buffer.from(String(right));
  return a.length === b.length && timingSafeEqual(a, b);
}
