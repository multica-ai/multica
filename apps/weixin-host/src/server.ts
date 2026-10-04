import { timingSafeEqual } from "node:crypto";
import http from "node:http";

import type { Installation, InstallationStore } from "./installation-store.ts";
import type { LoginManager, LoginOwner } from "./login-manager.ts";
import type { LoadedPlugin } from "./plugin.ts";
import type { Supervisor } from "./supervisor.ts";

/**
 * Control API the Multica backend calls; browsers never reach it. Every
 * request carries the shared secret. Access control (who may connect which
 * agent) is the backend's job — this API trusts its caller.
 */
export function createControlServer(deps: {
  secret: string;
  store: InstallationStore;
  logins: LoginManager;
  supervisor: Supervisor;
  loaded: LoadedPlugin;
}): http.Server {
  const { store, logins, supervisor, loaded } = deps;

  function publicInstallation(installation: Installation) {
    const { token: _token, ...rest } = installation;
    return { ...rest, ...supervisor.health(installation.id) };
  }

  /** Stops a relay and forgets the Weixin credentials behind it. */
  async function removeInstallation(installation: Installation) {
    await supervisor.stop(installation.id);
    store.remove(installation.id);
    // Another installation may have been re-created on the same account.
    if (!store.list().some((i) => i.account_id === installation.account_id)) {
      loaded.accounts.clearWeixinAccount(installation.account_id);
      loaded.accounts.unregisterWeixinAccountId(installation.account_id);
    }
  }

  async function handle(req: http.IncomingMessage, res: http.ServerResponse) {
    const url = new URL(req.url ?? "/", "http://weixin-host");
    const parts = url.pathname.split("/").filter(Boolean);
    const route = `${req.method} /${parts.map((p, i) => (i >= 2 && isId(p) ? ":id" : p)).join("/")}`;

    if (route === "GET /healthz") return send(res, 200, { ok: true });
    if (!authorized(req, deps.secret)) return send(res, 401, { error: "unauthorized" });

    switch (route) {
      case "GET /v1/installations": {
        const workspaceId = url.searchParams.get("workspace_id");
        const installations = store
          .list()
          .filter((i) => !workspaceId || i.workspace_id === workspaceId)
          .map(publicInstallation);
        return send(res, 200, { installations });
      }
      case "POST /v1/logins": {
        const owner = await readJson<Partial<LoginOwner>>(req);
        if (!owner.workspace_id || !owner.agent_id || !owner.user_id) {
          return send(res, 400, { error: "workspace_id, agent_id and user_id are required" });
        }
        return send(
          res,
          201,
          await logins.start({
            workspace_id: owner.workspace_id,
            agent_id: owner.agent_id,
            user_id: owner.user_id,
          }),
        );
      }
      case "GET /v1/logins/:id": {
        const status = logins.status(parts[2]!);
        return status ? send(res, 200, status) : send(res, 404, { error: "login not found" });
      }
      case "POST /v1/logins/:id/verify-code": {
        const body = await readJson<{ code?: string }>(req);
        if (!body.code?.trim()) return send(res, 400, { error: "code is required" });
        return logins.submitVerifyCode(parts[2]!, body.code)
          ? send(res, 200, logins.status(parts[2]!))
          : send(res, 409, { error: "login is not waiting for a verify code" });
      }
      case "POST /v1/installations": {
        const body = await readJson<{
          login_id?: string;
          workspace_id?: string;
          agent_id?: string;
          installer_user_id?: string;
          token?: string;
          token_id?: string;
        }>(req);
        const { login_id, workspace_id, agent_id, installer_user_id, token, token_id } = body;
        if (!login_id || !workspace_id || !agent_id || !installer_user_id || !token || !token_id) {
          return send(res, 400, { error: "missing field" });
        }
        const login = logins.status(login_id);
        if (!login) return send(res, 404, { error: "login not found" });
        if (
          login.workspace_id !== workspace_id ||
          login.agent_id !== agent_id ||
          login.user_id !== installer_user_id
        ) {
          return send(res, 409, { error: "login belongs to another agent or member" });
        }
        if (!login.account_id || !logins.consume(login_id)) {
          return send(res, 409, { error: "login is not connected or was already used" });
        }
        // Superseded installations: this installer's previous Weixin on the
        // same agent, the same bot account, and any whose credentials the
        // plugin cleared (it drops older bots of a Weixin user on re-login).
        const superseded = store
          .list()
          .filter(
            (i) =>
              (i.agent_id === agent_id && i.installer_user_id === installer_user_id) ||
              i.account_id === login.account_id ||
              !supervisor.hasAccount(i.account_id),
          );
        for (const old of superseded) {
          if (old.account_id === login.account_id) {
            // Same bot re-bound: keep the credentials the login just saved.
            await supervisor.stop(old.id);
            store.remove(old.id);
          } else {
            await removeInstallation(old);
          }
        }
        const installation = store.add({
          workspace_id,
          agent_id,
          installer_user_id,
          token,
          token_id,
          account_id: login.account_id,
        });
        supervisor.start(installation);
        return send(res, 201, {
          installation: publicInstallation(installation),
          superseded: superseded.map((i) => ({
            id: i.id,
            token_id: i.token_id,
            installer_user_id: i.installer_user_id,
          })),
        });
      }
      case "DELETE /v1/installations/:id": {
        const installation = store.get(parts[2]!);
        if (!installation) return send(res, 404, { error: "installation not found" });
        await removeInstallation(installation);
        return send(res, 200, publicInstallation(installation));
      }
      default:
        return send(res, 404, { error: "not found" });
    }
  }

  return http.createServer((req, res) => {
    handle(req, res).catch((err) => {
      send(res, 500, { error: err instanceof Error ? err.message : String(err) });
    });
  });
}

function isId(segment: string): boolean {
  return /^[0-9a-f-]{8,}$/i.test(segment);
}

function authorized(req: http.IncomingMessage, secret: string): boolean {
  const header = req.headers.authorization ?? "";
  const given = Buffer.from(header.replace(/^Bearer\s+/i, ""));
  const expected = Buffer.from(secret);
  return given.length === expected.length && timingSafeEqual(given, expected);
}

async function readJson<T>(req: http.IncomingMessage): Promise<T> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  const text = Buffer.concat(chunks).toString("utf-8");
  return (text ? JSON.parse(text) : {}) as T;
}

function send(res: http.ServerResponse, status: number, body: unknown): void {
  if (res.headersSent) return;
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}
