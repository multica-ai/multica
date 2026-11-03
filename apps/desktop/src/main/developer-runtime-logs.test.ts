// @vitest-environment node
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, describe, expect, it } from "vitest";
import { LOCAL_TIMING_PREFIX } from "@multica/tutti-agent-gui-host/local-timing";
import { WebDiagnosticLog, hashPeerIds, redactDiagnosticText, snapshotDaemonLog } from "./developer-runtime-logs";

const roots: string[] = [];
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "multica-runtime-log-")); roots.push(root); return root;
}
afterEach(async () => { await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true }))); });

describe("diagnostic text scrubbing", () => {
  it.each([
    "Bearer hidden-value", "Basic hidden-value", 'Authorization: Bearer hidden-value',
    '"api_key": "hidden-value"', 'ANTHROPIC_AUTH_TOKEN=hidden-value', "Cookie: session=hidden-value; other=private", "session_id='hidden-value'",
    "https://test.invalid/path?code=hidden-value&signature=hidden-value#token=hidden-value",
    "https://user:hidden-value@test.invalid/path", "data:image/png;base64,hidden-value", 'body={"arbitrary":"hidden-value"}',
    "mul_hidden-value", "sk-1234567890hidden-value", "ghp_1234567890hidden-value", "eyJhidden-value.payload-value.signature-value",
    "-----BEGIN PRIVATE KEY-----\nhidden-value\n-----END PRIVATE KEY-----",
  ])("removes credentials/payloads in %s", (text) => {
    expect(redactDiagnosticText(text)).not.toContain("hidden-value");
    expect(redactDiagnosticText(text)).toContain("REDACTED");
  });
  it.each([
    "dial /ip4/203.0.113.7/tcp/4001/p2p/peer failed", "dial /ip6/2001:db8::1/udp/4001/quic-v1 failed", "via /dns4/relay.test.invalid/tcp/443/wss",
    "all dials failed * [/ip4/203.0.113.7/tcp/4001] backoff",
  ])("removes the address in %s", (text) => {
    const redacted = redactDiagnosticText(text);
    expect(redacted).not.toMatch(/203\.0\.113\.7|2001:db8|relay\.test\.invalid/);
    expect(redacted).toContain("[REDACTED ADDR]");
  });
  it("leaves file paths that only look like addresses", () => {
    const line = "read /tmp/ip4/config failed";
    expect(redactDiagnosticText(line)).toBe(line);
  });
  it("hashes both forms of peer ID and nothing else", () => {
    const ed25519 = "12D3KooWLnZUpcaBwbz9uD1XsyyHnbXUrJRmxnsMiRnuCmvPix67", rsa = "QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N";
    expect(hashPeerIds(`dial ${ed25519} via ${rsa} failed task=task-1`, (id) => id.slice(0, 4))).toBe("dial peer:12D3 via peer:QmYy failed task=task-1");
  });
  it("keeps status, duration, identifiers and diagnostic messages", () => {
    const line = "12:34:56.789 ERR websocket closed task_id=task-1 code=429 duration=2s component=daemon";
    expect(redactDiagnosticText(line)).toBe(line);
  });
});

describe("Web renderer diagnostic logs", () => {
  it("persists sanitized logs across reopen and isolates accounts/API hosts", async () => {
    const root = await fixture();
    const log = new WebDiagnosticLog(root, "https://api.test", "user");
    log.console({ level: "error", message: "fetch failed token=hidden-value", sourceId: "http://localhost/app.js?key=hidden-value", lineNumber: 42 });
    log.console({ level: "info", message: LOCAL_TIMING_PREFIX + "ignored" });
    await log.flush();
    expect(await readFile(log.path, "utf8")).not.toContain("hidden-value");
    const reopened = new WebDiagnosticLog(root, "https://api.test", "user");
    const snapshot = await reopened.snapshot(0, Date.now() + 1000);
    expect(snapshot).toMatchObject({ status: "available", incomplete: false, records: [{ level: "error", message: "fetch failed token=[REDACTED]", source: "http://localhost/app.js?key=[REDACTED]", line: 42 }] });
    expect((await reopened.snapshot(Date.now() + 1000, Date.now() + 2000)).status).toBe("empty");
    expect((await new WebDiagnosticLog(root, "https://api.test", "other").snapshot(0, Date.now())).status).toBe("missing");
    expect((await new WebDiagnosticLog(root, "https://other.test", "user").snapshot(0, Date.now())).status).toBe("missing");
    if (process.platform !== "win32") expect((await stat(log.path)).mode & 0o777).toBe(0o600);
  });
  it("bounds writes, reads both rotated files and revalidates stored fields", async () => {
    const root = await fixture();
    const log = new WebDiagnosticLog(root, "api", "user", 600);
    for (let index = 0; index < 20; index++) log.console({ level: "info", message: `entry-${index}` });
    const snapshot = await log.snapshot(0, Date.now() + 1000);
    expect(snapshot.status).toBe("available");
    expect(snapshot.truncated).toBe(true);
    expect(snapshot.records.at(-1)?.message).toBe("entry-19");
    expect((await stat(log.path)).size).toBeLessThanOrEqual(600);
    expect((await stat(log.path + ".1")).size).toBeLessThanOrEqual(600);
    await writeFile(log.path, JSON.stringify({ at: new Date().toISOString(), level: "info", message: "Bearer hidden-value", env: "hidden-value" }) + "\ninvalid\n");
    const read = await log.snapshot(0, Date.now() + 1000);
    expect(read.incomplete).toBe(true);
    expect(JSON.stringify(read)).not.toContain("hidden-value");
  });
  it("reports I/O failures and drops oversized messages without leaking their tails", async () => {
    const root = await fixture();
    const log = new WebDiagnosticLog(root, "api", "user");
    log.console({ level: "warning", message: "x".repeat(20000) + "hidden-value" });
    expect((await log.snapshot(0, Date.now() + 1000)).records[0]?.message).toBe("[OVERSIZED LOG MESSAGE OMITTED]");
    const blocked = join(root, "blocked"); await writeFile(blocked, "not a directory");
    const failure = new WebDiagnosticLog(blocked, "api", "user");
    failure.console({ level: "error", message: "fail" });
    await expect(failure.flush()).resolves.toBeUndefined();
    expect(await failure.snapshot(0, Date.now())).toMatchObject({ status: "unreadable", incomplete: true });
  });
});

describe("daemon log snapshots", () => {
  it("reads the bounded profile tail, drops partial lines and redacts before returning", async () => {
    const root = await fixture(); const path = join(root, "daemon.log");
    await writeFile(path, "old record that is outside the tail\n12:34:56.789 ERR failed token=hidden-value\n12:34:57.001 INF connected\npartial secret");
    const snapshot = await snapshotDaemonLog(path, 100);
    expect(snapshot).toMatchObject({ status: "available", truncated: true, incomplete: true, scope: "desktop-profile", timeFilterApplied: false });
    expect(snapshot.text).toContain("connected");
    expect(snapshot.text).not.toMatch(/hidden-value|partial secret|old record/);
    expect(snapshot.modifiedAt).toBeDefined();
  });
  it("distinguishes unresolved, missing, empty and unreadable sources", async () => {
    const root = await fixture(); const path = join(root, "daemon.log");
    expect((await snapshotDaemonLog(null)).status).toBe("unavailable");
    expect((await snapshotDaemonLog(path)).status).toBe("missing");
    await writeFile(path, "");
    expect((await snapshotDaemonLog(path)).status).toBe("empty");
    expect(await snapshotDaemonLog(root)).toMatchObject({ status: "unreadable", incomplete: true });
  });
  it("does not return multiline private keys, long lines or a partial first line", async () => {
    const root = await fixture(); const path = join(root, "daemon.log");
    await writeFile(path, "-----BEGIN PRIVATE KEY-----\nhidden-value\n-----END PRIVATE KEY-----\n");
    expect((await snapshotDaemonLog(path)).text).not.toContain("hidden-value");
    await writeFile(path, "x".repeat(17000) + "hidden-value\n");
    expect((await snapshotDaemonLog(path)).text).toBe("[OVERSIZED LOG MESSAGE OMITTED]\n");
    expect((await snapshotDaemonLog(path, 20)).text).toBe("");
  });
});
