// @vitest-environment node
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { AppApprovals, isVerificationApp } from "./approvals";

const calculator = { id: "com.apple.calculator", name: "Calculator" };
const textEdit = { id: "com.apple.TextEdit", name: "TextEdit" };
const directories: string[] = [];
const file = () => {
  const directory = mkdtempSync(join(tmpdir(), "cu-approvals-"));
  directories.push(directory);
  return join(directory, "nested", "approvals.json");
};
afterEach(() => { for (const directory of directories.splice(0)) rmSync(directory, { recursive: true, force: true }); });

describe("isVerificationApp", () => {
  it("matches only verification environment bundle IDs", () => {
    expect(isVerificationApp("ai.multica.verification.0d2c9f3e-1111-4222-8333-444455556666")).toBe(true);
    expect(isVerificationApp("ai.multica.verification.")).toBe(false);
    expect(isVerificationApp("ai.multica.desktop")).toBe(false);
    expect(isVerificationApp("com.github.Electron")).toBe(false);
  });
});

describe("AppApprovals", () => {
  it("remembers apps per account across instances in a private file", () => {
    const path = file();
    const approvals = new AppApprovals(path);
    approvals.add("user-a", calculator);
    approvals.add("user-a", calculator);
    const reloaded = new AppApprovals(path);
    expect(reloaded.has("user-a", calculator.id)).toBe(true);
    expect(reloaded.has("user-b", calculator.id)).toBe(false);
    expect(reloaded.list("user-a")).toEqual([calculator]);
    expect(JSON.parse(readFileSync(path, "utf8"))).toEqual({ schemaVersion: 1, users: { "user-a": [calculator] } });
    expect(statSync(path).mode & 0o777).toBe(0o600);
  });

  it("removes one app or clears one account without touching others", () => {
    const path = file();
    const approvals = new AppApprovals(path);
    approvals.add("user-a", calculator);
    approvals.add("user-a", textEdit);
    approvals.add("user-b", textEdit);
    approvals.remove("user-a", calculator.id);
    expect(new AppApprovals(path).list("user-a")).toEqual([textEdit]);
    approvals.clear("user-a");
    const reloaded = new AppApprovals(path);
    expect(reloaded.list("user-a")).toEqual([]);
    expect(reloaded.list("user-b")).toEqual([textEdit]);
  });

  it("starts empty when the stored file is unreadable", () => {
    const path = file();
    new AppApprovals(path).add("user-a", calculator);
    writeFileSync(path, "{not json");
    expect(new AppApprovals(path).has("user-a", calculator.id)).toBe(false);
  });
});
