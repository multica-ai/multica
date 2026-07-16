import { createHash, timingSafeEqual } from "node:crypto";

import { executeSandbox } from "./sandbox.mjs";

const MAX_INPUT_BYTES = 512 << 10;
const json = (value, status = 200) => new Response(JSON.stringify(value), { status, headers: { "content-type": "application/json; charset=utf-8" } });

export async function createWorkerRuntime(options) {
  const fetchImpl = options.fetch ?? globalThis.fetch;
  const response = await fetchImpl(options.bundleUrl, { headers: { authorization: `Bearer ${options.bundleToken}` } });
  if (!response.ok) throw new Error("App bundle failed validation");
  const payload = await response.json();
	const { files, bundleSha256 } = validateFiles(payload.files, options.appId, options.version);
	if (options.expectedBundleSha256 && !safeEqual(bundleSha256, options.expectedBundleSha256)) throw new Error("App bundle failed validation");
  const manifest = JSON.parse(files.get("app.json").toString("utf8")).manifest;
  const backendSource = files.get(manifest.backend?.entry);
  if (!backendSource) throw new Error("App bundle failed validation");
  const backendModules = Object.fromEntries([...files.entries()]
    .filter(([path]) => path.startsWith("backend/"))
    .map(([path, content]) => [path, content.toString("utf8")]));

  return {
    async fetch(request) {
      const url = new URL(request.url);
      if (request.method === "GET" && url.pathname === "/healthz") return json({ status: "ok", app_id: options.appId, version: options.version, commit: options.workerCommit ?? "" });
      if (request.method !== "POST" || url.pathname !== "/invoke") return json({ error: "Not found" }, 404);
      if (!safeEqual(request.headers.get("x-multica-invoke-key") ?? "", options.invokeKey ?? "")) return json({ error: "Unauthorized" }, 401);
      const raw = await request.text();
      if (Buffer.byteLength(raw) > MAX_INPUT_BYTES) return json({ error: "Request is too large" }, 413);
      let input;
      try {
        input = JSON.parse(raw || "{}");
      } catch {
        return json({ error: "Request body must be valid JSON" }, 400);
      }
		let host = options.host;
		if (typeof options.hostFactory === "function") {
			if (typeof input?.grant_token !== "string" || !("input" in input)) return json({ error: "Unauthorized" }, 401);
			host = options.hostFactory(input.grant_token);
			input = input.input;
		}
      try {
        const output = await executeSandbox({ source: backendSource.toString("utf8"), modules: backendModules, input, host });
        return json(output);
      } catch (error) {
        console.error("mini-app worker invocation failed", { appId: options.appId, version: options.version, error });
        return json({ error: "App worker failed" }, 502);
      }
    },
  };
}

function validateFiles(entries, appId, version) {
  if (!Array.isArray(entries) || entries.length === 0 || entries.length > 100) throw new Error("App bundle failed validation");
  const files = new Map();
  let totalBytes = 0;
  for (const entry of entries) {
    if (typeof entry?.path !== "string" || entry.path.includes("..") || entry.path.includes("\\") || entry.path.startsWith("/") ||
      (entry.path !== "app.json" && !entry.path.startsWith("frontend/") && !entry.path.startsWith("backend/")) ||
      typeof entry?.content_base64 !== "string" || typeof entry?.sha256 !== "string" || files.has(entry.path)) {
      throw new Error("App bundle failed validation");
    }
    const content = Buffer.from(entry.content_base64, "base64");
    totalBytes += content.byteLength;
    if (content.byteLength > 512 << 10 || totalBytes > 5 << 20) throw new Error("App bundle failed validation");
    const actual = createHash("sha256").update(content).digest("hex");
    if (!safeEqual(actual, entry.sha256.toLowerCase())) throw new Error("App bundle failed validation");
    files.set(entry.path, content);
  }
  const appFile = files.get("app.json");
  if (!appFile) throw new Error("App bundle failed validation");
  try {
    const manifest = JSON.parse(appFile.toString("utf8")).manifest;
    if (manifest?.version !== version || typeof appId !== "string" || !files.has(manifest.frontend?.entry) || !files.has(manifest.backend?.entry)) throw new Error("invalid identity");
  } catch {
    throw new Error("App bundle failed validation");
  }
	const bundleHash = createHash("sha256");
	for (const entry of [...entries].sort((left, right) => left.path.localeCompare(right.path))) {
		bundleHash.update(entry.path).update("\0").update(entry.sha256.toLowerCase()).update("\0");
	}
	return { files, bundleSha256: bundleHash.digest("hex") };
}

function safeEqual(left, right) {
  const a = Buffer.from(String(left));
  const b = Buffer.from(String(right));
  return a.length === b.length && timingSafeEqual(a, b);
}
