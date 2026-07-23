import { createHmac } from "node:crypto";

import { mintBundleToken, signServiceRequest } from "./auth.mjs";

export class BackendClient {
  constructor(options = {}) {
    this.baseUrl = (options.baseUrl ?? process.env.CEREBRO_APPS_BACKEND_URL ?? "").replace(/\/$/, "");
    this.serviceKey = options.serviceKey ?? process.env.CEREBRO_APPS_RUNTIME_SERVICE_KEY ?? "";
    this.fetch = options.fetch ?? globalThis.fetch;
  }

  async deploymentInput(request) {
    return {
      ...request,
      backendUrl: this.baseUrl,
      bundleUrl: `${this.baseUrl}/api/cerebro/apps-internal/${request.appId}/${request.version}/bundle`,
      bundleToken: mintBundleToken(this.serviceKey, request.appId, request.version),
      invokeKey: this.invokeKey(request.appId, request.version),
    };
  }

  invokeKey(appId, version) {
    return createHmac("sha256", this.serviceKey).update(`invoke\n${appId}\n${version}`).digest("base64url");
  }

  async callback(appId, version, value) {
    const path = `/api/cerebro/apps-internal/${appId}/${version}/callback`;
    const body = Buffer.from(JSON.stringify(value));
    const signed = signServiceRequest(this.serviceKey, "POST", path, body);
    const response = await this.fetch(`${this.baseUrl}${path}`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-multica-timestamp": signed.timestamp,
        "x-multica-signature": signed.signature,
      },
      body,
    });
    if (!response.ok) throw new Error("Backend callback failed");
  }

  async pending() {
    const path = "/api/cerebro/apps-internal/deployments";
    const signed = signServiceRequest(this.serviceKey, "GET", path);
    const response = await this.fetch(`${this.baseUrl}${path}`, {
      headers: { "x-multica-timestamp": signed.timestamp, "x-multica-signature": signed.signature },
    });
    if (!response.ok) throw new Error("Backend deployment read failed");
    const rows = await response.json();
    return rows.map((row) => ({ appId: row.app_id, appName: row.app_name, version: row.version, bundleSha256: row.bundle_sha256 }));
  }

  async deployment(appId, version) {
    const path = `/api/cerebro/apps-internal/deployments/${appId}/${version}`;
    const signed = signServiceRequest(this.serviceKey, "GET", path);
    const response = await this.fetch(`${this.baseUrl}${path}`, {
      headers: { "x-multica-timestamp": signed.timestamp, "x-multica-signature": signed.signature },
    });
    if (!response.ok) throw new Error("Ready app deployment not found");
    const row = await response.json();
    if (!row.internal_domain?.endsWith(".internal")) throw new Error("Ready app deployment not found");
    return { serviceId: row.external_service_id, internalDomain: row.internal_domain, invokeKey: this.invokeKey(appId, version) };
  }

  async bundle(appId, version) {
    const path = `/api/cerebro/apps-internal/${appId}/${version}/bundle`;
    const signed = signServiceRequest(this.serviceKey, "GET", path);
    const response = await this.fetch(`${this.baseUrl}${path}`, {
      headers: { "x-multica-timestamp": signed.timestamp, "x-multica-signature": signed.signature },
    });
    if (!response.ok) throw new Error("App bundle read failed");
    return response.json();
  }
}
