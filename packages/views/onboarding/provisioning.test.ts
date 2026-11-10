import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import type { Agent, Workspace } from "@multica/core/types";
import {
  createDefaultWorkspace,
  defaultWorkspaceSlugBase,
  findReusableAgent,
  splitTaskText,
  uniqueAgentName,
} from "./provisioning";
import { parseInviteEmails } from "./getting-started/invite-teammates-dialog";

describe("uniqueAgentName", () => {
  it("keeps the tool name while it is free", () => {
    expect(uniqueAgentName("Claude Code", ["Codex"], "Jiayuan Zhang")).toBe("Claude Code");
  });

  it("adds the owner's first name when a teammate already took it", () => {
    expect(uniqueAgentName("Claude Code", ["Claude Code"], "Jiayuan Zhang")).toBe(
      "Claude Code · Jiayuan",
    );
  });

  it("counts up only when the personal name is taken too", () => {
    expect(
      uniqueAgentName("Claude Code", ["Claude Code", "Claude Code · Jiayuan"], "Jiayuan"),
    ).toBe("Claude Code · Jiayuan 2");
  });
});

describe("findReusableAgent", () => {
  const agent = (over: Partial<Agent>) =>
    ({ id: "a", runtime_id: "rt-1", owner_id: "u-1", archived_at: null, system_key: null, ...over }) as Agent;

  it("reuses the member's own agent on the same runtime", () => {
    expect(findReusableAgent([agent({ id: "mine" })], "rt-1", "u-1")?.id).toBe("mine");
  });

  it("ignores archived, system, other runtimes and other owners", () => {
    expect(
      findReusableAgent(
        [
          agent({ archived_at: "2026-01-01" }),
          agent({ system_key: "mika" } as Partial<Agent>),
          agent({ runtime_id: "rt-2" }),
          agent({ owner_id: "u-2" }),
        ],
        "rt-1",
        "u-1",
      ),
    ).toBeUndefined();
  });
});

describe("splitTaskText", () => {
  it("uses a one-line ask as the title alone", () => {
    expect(splitTaskText("  Fix the login bug \n")).toEqual({ title: "Fix the login bug" });
  });

  it("keeps a multi-line ask whole in the description", () => {
    expect(splitTaskText("Fix login\nIt 500s on SSO")).toEqual({
      title: "Fix login",
      description: "Fix login\nIt 500s on SSO",
    });
  });

  it("cuts an overlong first line", () => {
    const { title, description } = splitTaskText("x".repeat(200));
    expect(title).toHaveLength(120);
    expect(title.endsWith("…")).toBe(true);
    expect(description).toBe("x".repeat(200));
  });
});

describe("defaultWorkspaceSlugBase", () => {
  it("falls back to the email when the name gives no slug", () => {
    expect(defaultWorkspaceSlugBase({ name: "", email: "Ada.L@example.com" })).toBe("ada-l");
  });

  it("falls back to a generic base as a last resort", () => {
    expect(defaultWorkspaceSlugBase({ name: "", email: "@" })).toBe("workspace");
  });
});

describe("createDefaultWorkspace", () => {
  const ws = { id: "ws", slug: "ada" } as Workspace;
  const conflict = () => new ApiError("taken", 409, "Conflict");

  it("tries the plain slug first", async () => {
    const create = vi.fn().mockResolvedValue(ws);
    await createDefaultWorkspace({ name: "Ada's", slugBase: "ada", create });
    expect(create).toHaveBeenCalledWith({ name: "Ada's", slug: "ada" });
  });

  it("suffixes a reserved slug without trying it", async () => {
    const create = vi.fn().mockResolvedValue(ws);
    await createDefaultWorkspace({ name: "x", slugBase: "settings", create, random: () => 0 });
    expect(create).toHaveBeenCalledWith({ name: "x", slug: "settings-aaaa" });
  });

  it("retries conflicts, and gives up after a few", async () => {
    const create = vi.fn().mockRejectedValue(conflict());
    await expect(
      createDefaultWorkspace({ name: "x", slugBase: "ada", create }),
    ).rejects.toMatchObject({ status: 409 });
    expect(create).toHaveBeenCalledTimes(4);
  });

  it("does not retry other errors", async () => {
    const create = vi.fn().mockRejectedValue(new ApiError("no", 403, "Forbidden"));
    await expect(createDefaultWorkspace({ name: "x", slugBase: "ada", create })).rejects.toMatchObject({
      status: 403,
    });
    expect(create).toHaveBeenCalledOnce();
  });
});

describe("parseInviteEmails", () => {
  it("splits on commas, semicolons and whitespace, deduping case-insensitively", () => {
    expect(parseInviteEmails("a@x.io, B@x.io;\nb@x.io  nope")).toEqual({
      valid: ["a@x.io", "b@x.io"],
      invalid: ["nope"],
    });
  });
});
