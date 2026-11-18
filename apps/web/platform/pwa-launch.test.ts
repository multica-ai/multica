// @vitest-environment node
import { describe, expect, it } from "vitest";
import { resolveLaunchPath } from "./pwa-launch";

const ORIGIN = "https://www.multica.ai";

describe("resolveLaunchPath", () => {
  it.each([
    ["a plain relaunch stays on the current page", "/inbox", "/acme/issues/abc", null],
    ["the Inbox shortcut opens the current workspace's inbox", "/inbox?launch=shortcut", "/acme/issues/abc", "/acme/inbox"],
    ["the My Issues shortcut opens the current workspace's page", "/my-issues", "/acme/issues/abc", "/acme/my-issues"],
    ["a slug-less page outside a workspace is left to the proxy", "/my-issues", "/login", "/my-issues"],
    ["the My Issues shortcut leaves a filtered view of the page", "/my-issues", "/acme/my-issues?scope=agents", "/acme/my-issues"],
    ["a window already on the page stays", "/my-issues", "/acme/my-issues", null],
    ["the launch that opened a shortcut window stays", "/inbox?launch=shortcut", "/acme/inbox?launch=shortcut", null],
    ["a comment link keeps its anchor", "/acme/issues/MUL-9#comment-c-9", "/acme/inbox", "/acme/issues/MUL-9#comment-c-9"],
    ["a link to another comment on the open issue moves to it", "/acme/issues/MUL-9#comment-c-9", "/acme/issues/MUL-9#comment-c-8", "/acme/issues/MUL-9#comment-c-9"],
    ["the launch that opened a comment link stays", "/acme/issues/MUL-9#comment-c-9", "/acme/issues/MUL-9#comment-c-9", null],
    ["a link to another view of the open page moves to it", "/acme/my-issues?scope=agents", "/acme/my-issues", "/acme/my-issues?scope=agents"],
    ["a workspace-scoped target is used as is", "/beta/issues/xyz?tab=activity", "/acme/inbox", "/beta/issues/xyz?tab=activity"],
    ["a target on another origin is ignored", "https://other.example/acme/inbox", "/acme/issues", null],
  ])("%s", (_, target, current, expected) => {
    expect(
      resolveLaunchPath(new URL(target, ORIGIN).href, new URL(current, ORIGIN)),
    ).toBe(expected);
  });
});
