export class DeploymentManager {
  constructor({ provider, backend }) {
    this.provider = provider;
    this.backend = backend;
    this.inflight = new Map();
  }

  deploy(request) {
    const key = `${request.appId}@${request.version}`;
    const active = this.inflight.get(key);
    if (active) return active;
    const operation = this.#deploy(request).finally(() => this.inflight.delete(key));
    this.inflight.set(key, operation);
    return operation;
  }

  async #deploy(request) {
    try {
      const input = await this.backend.deploymentInput(request);
      const result = await this.provider.createOrDeploy(input);
      await this.backend.callback(request.appId, request.version, {
        status: "ready",
        external_service_id: result.serviceId,
        internal_domain: result.internalDomain,
      });
      await this.#reapSuperseded(request.appId, request.version);
      return result;
    } catch (error) {
      console.error("mini-app deployment manager failed", { appId: request.appId, version: request.version, error });
      await this.backend.callback(request.appId, request.version, { status: "failed", error: "App deployment failed" });
      throw new Error("App deployment failed");
    }
  }

  // Runs only after the new version is live and reported ready, so cleanup can
  // never take down a healthy deploy. Reaping is best-effort: a failure here
  // leaves stale services for the next deploy to sweep, it never fails the deploy.
  async #reapSuperseded(appId, version) {
    try {
      if (typeof this.provider.reapSuperseded !== "function") return;
      const reaped = await this.provider.reapSuperseded(appId, version);
      if (reaped) console.info("mini-app superseded versions reaped", { appId, version, reaped });
    } catch (error) {
      console.error("mini-app reap after deploy failed", { appId, version, error });
    }
  }

  async resume() {
    const pending = await this.backend.pending();
    await Promise.all(pending.map((request) => this.deploy(request)));
  }

  async lifecycle(action, serviceId) {
    if (action === "pause") return this.provider.pause(serviceId);
    if (action === "delete") return this.provider.delete(serviceId);
    throw new Error("Unsupported app lifecycle operation");
  }
}
