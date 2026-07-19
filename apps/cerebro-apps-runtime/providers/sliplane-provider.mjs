import { AppProvider } from "./provider.mjs";
import { createHash } from "node:crypto";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const SEMVER = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$/;
const COMMIT_SHA = /^[0-9a-f]{40}$/i;

export function serviceNameForVersion(appName, appId, version) {
  const humanName = String(appName ?? "").replace(/\s+/g, " ").trim();
  if (!humanName || /[\u0000-\u001f\u007f]/.test(humanName) || !UUID.test(appId) || !SEMVER.test(version)) {
    throw new Error("Invalid app version identity");
  }
  const identity = createHash("sha256").update(`${appId.toLowerCase()}@${version}`).digest("hex").slice(0, 8);
  return `${humanName} · ${version} · ${identity}`;
}

export class SliplaneProvider extends AppProvider {
  constructor(options = {}) {
    super();
    this.apiKey = options.apiKey ?? process.env.SLIPLANE_KEY ?? "";
    this.projectId = options.projectId ?? process.env.CEREBRO_APPS_SLIPLANE_PROJECT_ID ?? "";
    this.serverId = options.serverId ?? process.env.CEREBRO_APPS_SLIPLANE_SERVER_ID ?? "";
    this.workerBranch = options.workerBranch ?? process.env.CEREBRO_APPS_WORKER_BRANCH ?? "";
    this.workerCommit = options.workerCommit ?? process.env.CEREBRO_APPS_WORKER_COMMIT ?? "";
    this.baseUrl = (options.baseUrl ?? "https://ctrl.sliplane.io/v0").replace(/\/$/, "");
    this.fetch = options.fetch ?? globalThis.fetch;
    this.timeoutMs = options.timeoutMs ?? 10_000;
    this.healthPollMs = options.healthPollMs ?? 2_000;
    this.healthTimeoutMs = options.healthTimeoutMs ?? 120_000;
    this.workerPort = options.workerPort ?? 4311;
  }

  async createOrDeploy(deployment) {
    try {
      this.#requireConfiguration();
      const name = serviceNameForVersion(deployment.appName, deployment.appId, deployment.version);
      let service = await this.#findExistingDeployment(name, deployment);
      let created = false;
      if (!service) {
        service = await this.#createService(name, deployment);
        if (service?.id) {
          created = true;
        } else if (!service) {
          service = await this.#findExistingDeployment(name, deployment);
          if (!service) throw new Error("existing Sliplane service identity mismatch");
        }
      }
      if (!service?.id || service.serverId && service.serverId !== this.serverId) throw new Error("invalid Sliplane service");
      await this.#request(`/projects/${this.projectId}/services/${service.id}/deploy`, { method: "POST", body: "{}" }, [200, 202, 204]);
      const internalDomain = await this.#waitUntilHealthy(service.id, created ? null : service);
      return { serviceId: service.id, internalDomain };
    } catch (error) {
      console.error("Sliplane app deployment failed", { error, appId: deployment.appId, version: deployment.version });
      throw new Error("App deployment failed");
    }
  }

  async pause(serviceId) {
    try {
      await this.#request(`/projects/${this.projectId}/services/${serviceId}/pause`, { method: "POST", body: "{}" }, [200, 202, 204]);
    } catch (error) {
      console.error("Sliplane app pause failed", { error, serviceId });
      throw new Error("App lifecycle operation failed");
    }
  }

  async delete(serviceId) {
    try {
      await this.#request(`/projects/${this.projectId}/services/${serviceId}`, { method: "DELETE" }, [200, 202, 204, 404]);
    } catch (error) {
      console.error("Sliplane app delete failed", { error, serviceId });
      throw new Error("App lifecycle operation failed");
    }
  }

  // Delete every superseded worker service for an app so old versions do not
  // pile up on the host (each published version otherwise leaves a dead
  // service behind — the accumulation that overloaded the staging host).
  // Best-effort: a scan or per-service delete failure never rejects, so a
  // healthy just-deployed version is never taken down by cleanup.
  async reapSuperseded(appId, keepVersion) {
    if (!UUID.test(String(appId ?? "")) || !SEMVER.test(String(keepVersion ?? ""))) return 0;
    let reaped = 0;
    try {
      if (!this.apiKey || !this.projectId || !this.serverId || typeof this.fetch !== "function") throw new Error("Sliplane provider is not configured for reaping");
      const response = await this.#request(`/projects/${this.projectId}/services`, {}, [200], true);
      const services = await response.json();
      const superseded = services.filter((service) => this.#isSupersededAppService(service, appId, keepVersion));
      for (const service of superseded) {
        try {
          await this.#request(`/projects/${this.projectId}/services/${service.id}`, { method: "DELETE" }, [200, 202, 204, 404]);
          reaped++;
        } catch (error) {
          console.error("Sliplane superseded app service delete failed", { error, serviceId: service.id, appId });
        }
      }
    } catch (error) {
      console.error("Sliplane superseded app service scan failed", { error, appId });
    }
    return reaped;
  }

  #isSupersededAppService(service, appId, keepVersion) {
    if (!service?.id || service.serverId !== this.serverId) return false;
    const env = new Map((service.env ?? []).map((entry) => [entry.key, String(entry.value ?? "")]));
    if (env.get("APP_ID") !== appId) return false;
    const version = env.get("APP_VERSION");
    return Boolean(version) && version !== keepVersion;
  }

  async #createService(name, deployment) {
    const body = {
      name,
      serverId: this.serverId,
      deployment: {
        url: "https://github.com/firtal-group/firtal-cerebro",
        branch: this.workerBranch,
        dockerContext: "/",
        dockerfilePath: "apps/cerebro-apps-runtime/Dockerfile.worker",
        autoDeploy: false,
      },
      network: { public: false },
      env: [
        { key: "APP_ID", value: deployment.appId, secret: false },
        { key: "APP_VERSION", value: deployment.version, secret: false },
        { key: "BUNDLE_URL", value: deployment.bundleUrl ?? "", secret: false },
        { key: "BUNDLE_TOKEN", value: deployment.bundleToken ?? "", secret: true },
        { key: "BUNDLE_SHA256", value: deployment.bundleSha256 ?? "", secret: false },
        { key: "BACKEND_URL", value: deployment.backendUrl ?? "", secret: false },
        { key: "WORKER_COMMIT", value: this.workerCommit, secret: false },
        { key: "INVOKE_KEY", value: deployment.invokeKey ?? "", secret: true },
        { key: "PORT", value: "4311", secret: false },
      ],
    };
    const response = await this.#request(`/projects/${this.projectId}/services`, { method: "POST", body: JSON.stringify(body) }, [201, 409], true);
    if (response.status === 409) return null;
    return response.json();
  }

  async #findExistingDeployment(name, deployment) {
    const response = await this.#request(`/projects/${this.projectId}/services`, {}, [200], true);
    const services = await response.json();
    return services.find((service) => service.name === name && service.serverId === this.serverId && this.#matchesDeployment(service, deployment));
  }

  async #getService(serviceId) {
    const response = await this.#request(`/projects/${this.projectId}/services/${encodeURIComponent(serviceId)}`, {}, [200], true);
    return response.json();
  }

  #matchesDeployment(service, deployment) {
    if (!service) return false;
    const env = new Map((service.env ?? []).map((entry) => [entry.key, String(entry.value ?? "")]));
    return env.get("APP_ID") === deployment.appId
      && env.get("APP_VERSION") === deployment.version
      && env.get("BUNDLE_SHA256") === (deployment.bundleSha256 ?? "")
      && env.get("WORKER_COMMIT") === this.workerCommit;
  }

  async #waitUntilHealthy(serviceId, initialService = null) {
    const deadline = Date.now() + this.healthTimeoutMs;
    let service = initialService;
    while (Date.now() < deadline) {
      let internalDomain = "";
      try {
        service ??= await this.#getService(serviceId);
        if (!service?.id || service.serverId && service.serverId !== this.serverId) throw new Error("invalid Sliplane service");
        if (service.status === "failed") throw new Error("private app worker deployment failed");
        internalDomain = service.network?.internalDomain ?? service.internalDomain ?? "";
        if (!internalDomain.endsWith(".internal")) throw new Error("missing internal domain");
        if (service.status && service.status !== "live") throw new Error("private app worker is not live");
        const healthUrl = `http://${internalDomain}:${this.workerPort}/healthz`;
        const remaining = Math.max(1, deadline - Date.now());
        const response = await this.fetch(healthUrl, { signal: AbortSignal.timeout(Math.min(this.timeoutMs, remaining)) });
        if (response.ok) {
          const body = await response.json().catch(() => null);
          if (body?.status === "ok" && body?.commit === this.workerCommit) return internalDomain;
        }
      } catch (error) {
        console.error("Sliplane app health check failed", { error, serviceId, internalDomain });
      }
      service = null;
      await new Promise((resolve) => setTimeout(resolve, Math.min(this.healthPollMs, Math.max(0, deadline - Date.now()))));
    }
    throw new Error("private app worker did not become healthy");
  }

  async #request(path, init, accepted, returnResponse = false) {
    const response = await this.fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        authorization: `Bearer ${this.apiKey}`,
        "content-type": "application/json",
        ...init.headers,
      },
      signal: init.signal ?? AbortSignal.timeout(this.timeoutMs),
    });
    if (!accepted.includes(response.status)) throw new Error(`Sliplane returned ${response.status}`);
    return returnResponse ? response : response;
  }

  #requireConfiguration() {
    if (!this.apiKey || !this.projectId || !this.serverId || !this.workerBranch || !COMMIT_SHA.test(this.workerCommit) || typeof this.fetch !== "function") throw new Error("Sliplane provider is not configured");
  }
}
