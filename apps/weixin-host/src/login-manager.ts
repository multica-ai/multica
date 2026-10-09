import { randomUUID } from "node:crypto";

import type { WeixinPlugin } from "./plugin.ts";

/** How long a web QR login may take before the host gives up. */
const LOGIN_TIMEOUT_MS = 5 * 60 * 1000;
/** Finished logins stay readable this long so the UI can collect the result. */
const RESULT_TTL_MS = 10 * 60 * 1000;

export type LoginState =
  | "waiting"
  | "scanned"
  | "need_verify_code"
  | "connected"
  | "failed";

export interface LoginStatus {
  id: string;
  state: LoginState;
  /** Content to render as the QR code; replaced when the plugin refreshes it. */
  qr_content: string;
  message: string;
  /** The last verify code was rejected. */
  verify_code_invalid: boolean;
  /** Normalized plugin account id, once connected. */
  account_id?: string;
  /** Who started the login; only they may turn it into an installation. */
  workspace_id: string;
  agent_id: string;
  user_id: string;
  /** Already turned into an installation. */
  consumed: boolean;
}

export type LoginOwner = Pick<LoginStatus, "workspace_id" | "agent_id" | "user_id">;

interface Login extends LoginStatus {
  finishedAt?: number;
}

/**
 * Drives the plugin's QR login for the web UI.
 *
 * The plugin was written for a terminal: it reports progress (scanned, a
 * refreshed QR link, the verify-code prompt) by writing to stdout and reads
 * the phone's verify code from stdin. This class is the one place that
 * bridges those to HTTP — it recognizes the plugin's output on stdout and
 * feeds a verify code typed in the browser to stdin. Prompts are
 * attributed to the newest unfinished login, so two logins that need a verify
 * code at the same moment would collide; the plugin gives no way to tell
 * them apart.
 */
export class LoginManager {
  private readonly plugin: WeixinPlugin;
  private readonly normalizeAccountId: (raw: string) => string;
  private readonly onConnected: (accountId: string) => void;
  private readonly logins = new Map<string, Login>();
  private expectQrLink = false;

  constructor(opts: {
    plugin: WeixinPlugin;
    normalizeAccountId: (raw: string) => string;
    onConnected?: (accountId: string) => void;
  }) {
    this.plugin = opts.plugin;
    this.normalizeAccountId = opts.normalizeAccountId;
    this.onConnected = opts.onConnected ?? (() => {});
    this.watchPluginOutput();
  }

  async start(owner: LoginOwner): Promise<LoginStatus> {
    this.purge();
    const id = randomUUID();
    // The plugin keys the login by `accountId` when given one.
    const started = await this.plugin.gateway.loginWithQrStart({ accountId: id, force: true });
    if (!started.qrDataUrl) {
      throw new Error(started.message || "failed to get a Weixin QR code");
    }
    const login: Login = {
      id,
      state: "waiting",
      qr_content: started.qrDataUrl,
      message: started.message,
      verify_code_invalid: false,
      ...owner,
      consumed: false,
    };
    this.logins.set(id, login);
    void this.wait(login);
    return toStatus(login);
  }

  status(id: string): LoginStatus | undefined {
    const login = this.logins.get(id);
    return login ? toStatus(login) : undefined;
  }

  /** Claims a connected login for one installation; false if already claimed. */
  consume(id: string): boolean {
    const login = this.logins.get(id);
    if (!login || login.state !== "connected" || login.consumed) return false;
    login.consumed = true;
    return true;
  }

  submitVerifyCode(id: string, code: string): boolean {
    const login = this.logins.get(id);
    if (!login || login.state !== "need_verify_code") return false;
    login.state = "scanned";
    login.verify_code_invalid = false;
    process.stdin.emit("data", `${code.trim()}\n`);
    return true;
  }

  private async wait(login: Login): Promise<void> {
    try {
      const result = await this.plugin.gateway.loginWithQrWait({
        sessionKey: login.id,
        timeoutMs: LOGIN_TIMEOUT_MS,
      });
      if (result.connected && result.accountId) {
        login.state = "connected";
        login.account_id = this.normalizeAccountId(result.accountId);
        this.onConnected(login.account_id);
      } else {
        login.state = "failed";
      }
      login.message = result.message;
    } catch (err) {
      login.state = "failed";
      login.message = err instanceof Error ? err.message : String(err);
    }
    login.finishedAt = Date.now();
  }

  private watchPluginOutput(): void {
    const write = process.stdout.write.bind(process.stdout);
    process.stdout.write = ((chunk: unknown, ...rest: unknown[]) => {
      if (typeof chunk === "string") this.onPluginOutput(chunk);
      return (write as (...args: unknown[]) => boolean)(chunk, ...rest);
    }) as typeof process.stdout.write;
  }

  private onPluginOutput(text: string): void {
    const login = [...this.logins.values()].reverse().find((l) => !isFinished(l));
    if (!login) return;
    if (this.expectQrLink && /^https?:\/\//.test(text.trim())) {
      // A refreshed QR: the plugin prints its link right after the
      // "visit this link" line (see its displayQRCode).
      this.expectQrLink = false;
      login.qr_content = text.trim();
      login.state = "waiting";
      return;
    }
    this.expectQrLink = text.includes("以下链接以继续");
    if (text.includes("输入手机微信显示的数字")) {
      login.state = "need_verify_code";
      login.verify_code_invalid = text.includes("不匹配");
    } else if (text.includes("正在验证")) {
      login.state = "scanned";
    }
  }

  private purge(): void {
    const now = Date.now();
    for (const [id, login] of this.logins) {
      if (login.finishedAt && now - login.finishedAt > RESULT_TTL_MS) this.logins.delete(id);
    }
  }
}

function isFinished(login: Login): boolean {
  return login.state === "connected" || login.state === "failed";
}

function toStatus(login: Login): LoginStatus {
  const { finishedAt: _finishedAt, ...status } = login;
  return status;
}
