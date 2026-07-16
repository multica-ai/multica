import { createHash } from "node:crypto";

const SAFE_PATH = /^(?:app\.json|frontend\/[^/](?:.*[^/])?|backend\/[^/](?:.*[^/])?)$/;

export class BundleStore {
  constructor({ backend }) {
    this.backend = backend;
    this.bundles = new Map();
  }

  async get(appId, version, path) {
    const key = `${appId}@${version}`;
    if (!this.bundles.has(key)) this.bundles.set(key, this.#load(appId, version).catch((error) => {
      this.bundles.delete(key);
      throw error;
    }));
    const files = await this.bundles.get(key);
    const file = files.get(path);
    if (!file) throw new Error("App bundle file not found");
    return file;
  }

  async #load(appId, version) {
    const bundle = await this.backend.bundle(appId, version);
    if (!Array.isArray(bundle?.files)) throw new Error("App bundle failed validation");
    const files = new Map();
    try {
      for (const entry of bundle.files) {
        if (typeof entry?.path !== "string" || !SAFE_PATH.test(entry.path) || entry.path.includes("..") || files.has(entry.path)) throw new Error();
        if (typeof entry.content_base64 !== "string" || typeof entry.sha256 !== "string") throw new Error();
        const content = Buffer.from(entry.content_base64, "base64");
        const hash = createHash("sha256").update(content).digest("hex");
        if (hash !== entry.sha256.toLowerCase()) throw new Error();
        files.set(entry.path, { content, mediaType: entry.media_type ?? "application/octet-stream", sha256: hash });
      }
    } catch {
      throw new Error("App bundle failed validation");
    }
    return files;
  }
}
