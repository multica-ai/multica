import { mkdtempSync, rmSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import {
  appSuffixForPath,
  applyWorktreeDevEnv,
  cksum,
  offsetForPath,
  rendererPortForPath,
} from "./worktree-dev-env.mjs";

const cleanups = [];
afterEach(() => {
  while (cleanups.length) cleanups.pop()();
});

function tmpRoot(kind /* "file" | "dir" | "none" */) {
  const root = mkdtempSync(join(tmpdir(), "wt-"));
  cleanups.push(() => rmSync(root, { recursive: true, force: true }));
  if (kind === "file") writeFileSync(join(root, ".git"), "gitdir: /elsewhere\n");
  else if (kind === "dir") mkdirSync(join(root, ".git"));
  return root;
}

describe("worktree-dev-env", () => {
  it("cksum is byte-compatible with coreutils cksum(1)", () => {
    // `printf '%s' "/tmp/foo" | cksum` → 427878967 8
    expect(cksum(Buffer.from("/tmp/foo"))).toBe(427878967);
    // `printf '' | cksum` → 4294967295 0
    expect(cksum(Buffer.from(""))).toBe(4294967295);
  });

  it("derives the offset from the path, mod 1000", () => {
    expect(offsetForPath("/tmp/foo")).toBe(427878967 % 1000);
  });

  it("renderer port is 5173 + offset", () => {
    expect(rendererPortForPath("/tmp/foo")).toBe(5173 + (427878967 % 1000));
  });

  it("slugifies the worktree folder name", () => {
    expect(appSuffixForPath("/work/MUL-3724_Desktop")).toBe("mul-3724-desktop");
    expect(appSuffixForPath("/work/feat/some thing")).toBe("some-thing");
    expect(appSuffixForPath("/work/___")).toBe("worktree");
  });

  it("auto-isolates a linked worktree (.git is a file)", () => {
    const root = tmpRoot("file");
    const env = {};
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBe(String(rendererPortForPath(root)));
    expect(env.DESKTOP_APP_SUFFIX).toBe(appSuffixForPath(root));
  });

  it("leaves the primary checkout untouched (.git is a dir)", () => {
    const root = tmpRoot("dir");
    const env = {};
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBeUndefined();
    expect(env.DESKTOP_APP_SUFFIX).toBeUndefined();
  });

  it("respects explicit env overrides", () => {
    const root = tmpRoot("file");
    const env = { DESKTOP_RENDERER_PORT: "9999", DESKTOP_APP_SUFFIX: "manual" };
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBe("9999");
    expect(env.DESKTOP_APP_SUFFIX).toBe("manual");
  });

  it("fills only the missing knob when one is set explicitly", () => {
    const root = tmpRoot("file");
    const env = { DESKTOP_RENDERER_PORT: "9999" };
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBe("9999");
    expect(env.DESKTOP_APP_SUFFIX).toBe(appSuffixForPath(root));
  });
});
