// @vitest-environment node
import fs from "node:fs";
import type { AddressInfo } from "node:net";
import os from "node:os";
import path from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { InstallationStore } from "./installation-store.ts";
import { LoginManager, type LoginOwner } from "./login-manager.ts";
import type { LoadedPlugin, WeixinPlugin } from "./plugin.ts";
import { createControlServer } from "./server.ts";
import type { Supervisor } from "./supervisor.ts";

const owner: LoginOwner = { workspace_id: "ws-1", agent_id: "agent-1", user_id: "user-1" };

/** A plugin whose login waits until the test resolves it. */
function fakePlugin() {
  let finish: (r: { connected: boolean; message: string; accountId?: string }) => void = () => {};
  const plugin = {
    gateway: {
      loginWithQrStart: vi.fn(async () => ({ qrDataUrl: "https://qr/1", message: "scan" })),
      loginWithQrWait: vi.fn(
        () =>
          new Promise<{ connected: boolean; message: string; accountId?: string }>((resolve) => {
            finish = resolve;
          }),
      ),
    },
  } as unknown as WeixinPlugin;
  return { plugin, finish: (r: Parameters<typeof finish>[0]) => finish(r) };
}

const flush = () => new Promise((r) => setTimeout(r, 0));

describe("LoginManager", () => {
  it("tracks the plugin's terminal output as login state", async () => {
    const { plugin, finish } = fakePlugin();
    const logins = new LoginManager({ plugin, normalizeAccountId: (id) => id.replace("@", "-") });
    const login = await logins.start(owner);
    expect(login).toMatchObject({ state: "waiting", qr_content: "https://qr/1", ...owner });

    process.stdout.write("\n正在验证\n");
    expect(logins.status(login.id)?.state).toBe("scanned");

    // A refreshed QR: the plugin prints the fallback line, then the link.
    process.stdout.write("若二维码未能显示或无法使用，你可以访问以下链接以继续：\n");
    process.stdout.write("https://qr/2\n");
    expect(logins.status(login.id)).toMatchObject({ state: "waiting", qr_content: "https://qr/2" });

    process.stdout.write("❌ 你输入的数字不匹配，请重新输入：输入手机微信显示的数字");
    expect(logins.status(login.id)).toMatchObject({
      state: "need_verify_code",
      verify_code_invalid: true,
    });

    finish({ connected: true, message: "ok", accountId: "abc@im.bot" });
    await flush();
    expect(logins.status(login.id)).toMatchObject({ state: "connected", account_id: "abc-im.bot" });
  });

  it("feeds a verify code to the plugin's stdin reader", async () => {
    const { plugin } = fakePlugin();
    const logins = new LoginManager({ plugin, normalizeAccountId: (id) => id });
    const login = await logins.start(owner);
    const received: string[] = [];
    const onData = (chunk: string) => received.push(chunk);
    process.stdin.on("data", onData);

    expect(logins.submitVerifyCode(login.id, "12")).toBe(false);
    process.stdout.write("输入手机微信显示的数字，以继续连接：");
    expect(logins.submitVerifyCode(login.id, " 42 ")).toBe(true);

    process.stdin.off("data", onData);
    expect(received).toEqual(["42\n"]);
    expect(logins.status(login.id)?.state).toBe("scanned");
  });

  it("lets a connected login be consumed once", async () => {
    const { plugin, finish } = fakePlugin();
    const logins = new LoginManager({ plugin, normalizeAccountId: (id) => id });
    const login = await logins.start(owner);
    expect(logins.consume(login.id)).toBe(false);
    finish({ connected: true, message: "ok", accountId: "bot" });
    await flush();
    expect(logins.consume(login.id)).toBe(true);
    expect(logins.consume(login.id)).toBe(false);
  });
});

describe("control server", () => {
  let stateDir: string;
  let baseUrl: string;
  let close: () => void;
  let store: InstallationStore;
  let logins: LoginManager;
  let finish: ReturnType<typeof fakePlugin>["finish"];
  const supervisor = {
    start: vi.fn(),
    stop: vi.fn(async () => {}),
    hasAccount: vi.fn(() => true),
    health: () => ({ running: true, last_error: null }),
  };
  const accounts = { clearWeixinAccount: vi.fn(), unregisterWeixinAccountId: vi.fn() };

  beforeEach(async () => {
    vi.clearAllMocks();
    stateDir = fs.mkdtempSync(path.join(os.tmpdir(), "weixin-host-control-"));
    store = new InstallationStore(stateDir);
    const fake = fakePlugin();
    finish = fake.finish;
    logins = new LoginManager({ plugin: fake.plugin, normalizeAccountId: (id) => id });
    const server = createControlServer({
      secret: "s3cret",
      store,
      logins,
      supervisor: supervisor as unknown as Supervisor,
      loaded: { accounts } as unknown as LoadedPlugin,
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    baseUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
    close = () => server.close();
  });

  afterEach(() => {
    close();
    fs.rmSync(stateDir, { recursive: true, force: true });
  });

  const call = (method: string, p: string, body?: unknown, secret = "s3cret") =>
    fetch(`${baseUrl}${p}`, {
      method,
      headers: { Authorization: `Bearer ${secret}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });

  async function connectedLogin() {
    const login = (await (await call("POST", "/v1/logins", owner)).json()) as { id: string };
    finish({ connected: true, message: "ok", accountId: "bot-1" });
    await flush();
    return login.id;
  }

  const install = (loginId: string, overrides: Record<string, string> = {}) =>
    call("POST", "/v1/installations", {
      login_id: loginId,
      workspace_id: "ws-1",
      agent_id: "agent-1",
      installer_user_id: "user-1",
      token: "mul_secret_token",
      token_id: "tok-1",
      ...overrides,
    });

  it("rejects requests without the shared secret", async () => {
    expect((await call("GET", "/v1/installations", undefined, "wrong")).status).toBe(401);
    expect((await fetch(`${baseUrl}/healthz`)).status).toBe(200);
  });

  it("installs a connected login once and never returns the token", async () => {
    const loginId = await connectedLogin();

    const res = await install(loginId);
    expect(res.status).toBe(201);
    const body = await res.text();
    expect(body).not.toContain("mul_secret_token");
    expect(supervisor.start).toHaveBeenCalledTimes(1);

    expect((await install(loginId)).status).toBe(409);
    const list = await (await call("GET", "/v1/installations?workspace_id=ws-1")).text();
    expect(list).not.toContain("mul_secret_token");
    expect(JSON.parse(list).installations).toHaveLength(1);
  });

  it("refuses to install a login for another member or agent", async () => {
    const loginId = await connectedLogin();
    expect((await install(loginId, { installer_user_id: "user-2" })).status).toBe(409);
    expect((await install(loginId, { agent_id: "agent-2" })).status).toBe(409);
    expect(supervisor.start).not.toHaveBeenCalled();
  });

  it("replaces the member's previous Weixin on the same agent", async () => {
    const old = store.add({
      workspace_id: "ws-1",
      agent_id: "agent-1",
      installer_user_id: "user-1",
      account_id: "old-bot",
      token: "old",
      token_id: "tok-old",
    });
    const loginId = await connectedLogin();

    const res = (await (await install(loginId)).json()) as {
      superseded: { id: string; token_id: string }[];
    };

    expect(res.superseded).toEqual([
      { id: old.id, token_id: "tok-old", installer_user_id: "user-1" },
    ]);
    expect(accounts.clearWeixinAccount).toHaveBeenCalledWith("old-bot");
    expect(store.list().map((i) => i.account_id)).toEqual(["bot-1"]);
  });

  it("deletes an installation and forgets its Weixin credentials", async () => {
    const inst = store.add({
      workspace_id: "ws-1",
      agent_id: "agent-1",
      installer_user_id: "user-1",
      account_id: "bot-1",
      token: "t",
      token_id: "tok-1",
    });
    const res = await call("DELETE", `/v1/installations/${inst.id}`);
    expect(res.status).toBe(200);
    expect(supervisor.stop).toHaveBeenCalledWith(inst.id);
    expect(accounts.unregisterWeixinAccountId).toHaveBeenCalledWith("bot-1");
    expect(store.list()).toEqual([]);
  });
});
