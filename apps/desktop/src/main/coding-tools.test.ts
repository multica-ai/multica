// @vitest-environment node
import { EventEmitter } from "events";
import { PassThrough } from "stream";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { CodingToolRunEvent } from "../shared/coding-tools";

interface Script {
  output?: string;
  code: number;
  /** Leave the process running until the test closes it. */
  hold?: boolean;
}

const context = vi.hoisted(() => ({
  events: [] as unknown[],
  scripts: [] as Array<{ match: (args: string[]) => boolean; script: Script }>,
  spawned: [] as Array<{ file: string; args: string[]; child: unknown }>,
}));

vi.mock("electron", () => ({
  app: { on: vi.fn(), isPackaged: true },
  ipcMain: { handle: vi.fn() },
  BrowserWindow: {
    getAllWindows: () => [
      { isDestroyed: () => false, webContents: { send: (_channel: string, payload: unknown) => context.events.push(payload) } },
    ],
  },
}));

vi.mock("fs/promises", async (original) => ({
  ...(await original<typeof import("fs/promises")>()),
  access: vi.fn(async (path: string) => {
    if (!path.endsWith("/claude")) throw new Error("ENOENT");
  }),
}));

vi.mock("child_process", () => ({
  spawn: (file: string, args: string[]) => {
    const child = Object.assign(new EventEmitter(), {
      stdout: new PassThrough(),
      stderr: new PassThrough(),
      stdin: new PassThrough(),
      // No pid, so cancel never signals a real process group.
      pid: undefined,
      exitCode: null as number | null,
      signalCode: null,
      kill: vi.fn(),
      close(code: number) {
        child.exitCode = code;
        child.emit("close", code);
      },
    });
    context.spawned.push({ file, args, child });
    const found = context.scripts.find((entry) => entry.match(args));
    const script = found?.script ?? { code: 0 };
    setTimeout(() => {
      if (script.output) child.stdout.write(script.output);
      if (!script.hold) setTimeout(() => child.close(script.code), 5);
    }, 0);
    return child;
  },
}));

const {
  cancelCodingToolRun,
  codexAssetName,
  extractLoginUrl,
  loginNeedsCode,
  parseLoggedIn,
  parseVersion,
  startCodingToolRun,
  submitCodingToolCode,
} = await import("./coding-tools");

function lastEvent(): CodingToolRunEvent {
  return context.events[context.events.length - 1] as CodingToolRunEvent;
}

async function until(check: () => boolean) {
  await vi.waitFor(() => {
    if (!check()) throw new Error("not yet");
  });
}

afterEach(() => {
  context.events = [];
  context.scripts = [];
  context.spawned = [];
});

describe("output parsing", () => {
  it("finds the sign-in URL behind terminal hyperlinks", () => {
    const url = "https://claude.com/cai/oauth/authorize?code=true&state=abc";
    const output = `Opening browser to sign in…\nIf the browser didn't open, visit: \u001b]8;;${url}\u001b\\${url}\u001b]8;;\u001b\\\nPaste code here if prompted > `;
    expect(extractLoginUrl(output)).toBe(url);
    expect(loginNeedsCode(output)).toBe(true);
  });

  it("does not ask for a code when the CLI finishes in the browser", () => {
    const output = "Starting local login server on http://localhost:1455.\nhttps://auth.openai.com/oauth/authorize?x=1\n";
    expect(extractLoginUrl(output)).toBe("https://auth.openai.com/oauth/authorize?x=1");
    expect(loginNeedsCode(output)).toBe(false);
  });

  it("reads each tool's sign-in status", () => {
    expect(parseLoggedIn("claude", { code: 1, stdout: '{"loggedIn": false, "authMethod": "none"}', stderr: "" })).toBe(false);
    expect(parseLoggedIn("claude", { code: 0, stdout: '{\n  "loggedIn": true\n}', stderr: "" })).toBe(true);
    expect(parseLoggedIn("claude", { code: 0, stdout: "garbage", stderr: "" })).toBeNull();
    expect(parseLoggedIn("codex", { code: 0, stdout: "Logged in using ChatGPT", stderr: "" })).toBe(true);
    expect(parseLoggedIn("codex", { code: 1, stdout: "", stderr: "Not logged in" })).toBe(false);
    expect(parseLoggedIn("cursor", { code: 0, stdout: "\u001b[32m✓\u001b[0m Logged in as a@b.c", stderr: "" })).toBe(true);
    expect(parseLoggedIn("cursor", { code: 0, stdout: "Not logged in", stderr: "" })).toBe(false);
    expect(parseLoggedIn("cursor", { code: 0, stdout: "", stderr: "" })).toBeNull();
  });

  it("reads versions", () => {
    expect(parseVersion("2.1.300 (Claude Code)")).toBe("2.1.300");
    expect(parseVersion("codex-cli 0.170.0")).toBe("0.170.0");
    expect(parseVersion("2026.11.22-e9042b2")).toBe("2026.11.22-e9042b2");
    expect(parseVersion("")).toBeNull();
  });

  it("picks the Codex build for this machine", () => {
    expect(codexAssetName("darwin", "arm64")).toBe("codex-aarch64-apple-darwin");
    expect(codexAssetName("darwin", "x64")).toBe("codex-x86_64-apple-darwin");
    expect(codexAssetName("linux", "arm64")).toBe("codex-aarch64-unknown-linux-musl");
    expect(codexAssetName("win32", "x64")).toBeNull();
    expect(codexAssetName("linux", "ia32")).toBeNull();
  });
});

describe("runs", () => {
  it("rejects requests it does not understand", () => {
    expect(startCodingToolRun("gemini", "install")).toEqual({ error: expect.objectContaining({ code: "unknown" }) });
    expect(startCodingToolRun("claude", "uninstall")).toEqual({ error: expect.objectContaining({ code: "unknown" }) });
    expect(submitCodingToolCode("missing", "code")).toBe(false);
  });

  it("signs in with a pasted code, then runs a test prompt", async () => {
    const url = "https://claude.com/cai/oauth/authorize?state=1";
    context.scripts.push(
      { match: (args) => args[0] === "auth" && args[1] === "status", script: { code: 1, output: '{"loggedIn": false}' } },
      { match: (args) => args[0] === "auth" && args[1] === "login", script: { code: 0, hold: true, output: `visit: ${url}\nPaste code here if prompted > ` } },
      { match: (args) => args[0] === "-p", script: { code: 0, output: "OK" } },
    );
    const started = startCodingToolRun("claude", "login");
    if (!("runId" in started)) throw new Error("expected a run");

    await until(() => context.events.some((event) => (event as CodingToolRunEvent).needsCode));
    expect(lastEvent()).toMatchObject({ runId: started.runId, phase: "login", loginUrl: url, needsCode: true });
    expect(startCodingToolRun("claude", "login")).toEqual({ error: expect.objectContaining({ code: "busy" }) });

    expect(submitCodingToolCode(started.runId, "bad\ncode")).toBe(false);
    expect(submitCodingToolCode(started.runId, "  abc123  ")).toBe(true);
    const login = context.spawned.find((entry) => entry.args[1] === "login")!.child as {
      stdin: PassThrough;
      close: (code: number) => void;
    };
    expect(login.stdin.read()?.toString()).toBe("abc123\n");
    login.close(0);

    await until(() => lastEvent().finished === true);
    expect(lastEvent()).toMatchObject({ phase: "verify", status: "done" });
    expect(context.spawned.some((entry) => entry.args.includes("-p"))).toBe(true);
  });

  it("reports a cancelled sign-in", async () => {
    context.scripts.push(
      { match: (args) => args[1] === "status", script: { code: 1, output: '{"loggedIn": false}' } },
      { match: (args) => args[1] === "login", script: { code: 0, hold: true, output: "https://example.test/login\n" } },
    );
    const started = startCodingToolRun("claude", "login");
    if (!("runId" in started)) throw new Error("expected a run");
    await until(() => context.events.some((event) => (event as CodingToolRunEvent).loginUrl));

    cancelCodingToolRun(started.runId);
    const login = context.spawned.find((entry) => entry.args[1] === "login")!.child as { close: (code: number) => void };
    login.close(143);

    await until(() => lastEvent().finished === true);
    expect(lastEvent()).toMatchObject({ status: "failed", error: { code: "cancelled" } });
  });
});
