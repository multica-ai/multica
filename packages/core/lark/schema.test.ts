import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { DEFAULT_LARK_NOTIFICATION_EVENTS } from "../types/lark";
import {
  createEmptyListLarkInstallationsResponse,
  LarkInstallationSchema,
  ListLarkInstallationsResponseSchema,
} from "./schema";

const baseInstallation = {
  id: "installation-1",
  workspace_id: "workspace-1",
  agent_id: null,
  runtime_id: null,
  app_id: "app-1",
  tenant_key: "tenant-1",
  bot_open_id: "bot-1",
  installer_user_id: "user-1",
  status: "active",
  installation_kind: "notification",
  region: "feishu",
  installed_at: "2026-07-13T00:00:00Z",
  created_at: "2026-07-13T00:00:00Z",
  updated_at: "2026-07-13T00:00:00Z",
};

describe("LarkInstallationSchema notification events", () => {
  it("freezes the exported default selection", () => {
    expect(Object.isFrozen(DEFAULT_LARK_NOTIFICATION_EVENTS)).toBe(true);
  });

  it("preserves a valid event selection", () => {
    const parsed = LarkInstallationSchema.parse({
      ...baseInstallation,
      notification_event_types: ["mentioned", "issue_subscribed"],
    });

    expect(parsed.notification_event_types).toEqual(["mentioned", "issue_subscribed"]);
  });

  it("defaults a missing selection without exposing the shared default", () => {
    const first = LarkInstallationSchema.parse(baseInstallation);
    const second = LarkInstallationSchema.parse(baseInstallation);

    expect(first.notification_event_types).toEqual(DEFAULT_LARK_NOTIFICATION_EVENTS);
    expect(first.notification_event_types).not.toBe(DEFAULT_LARK_NOTIFICATION_EVENTS);
    expect(first.notification_event_types).not.toBe(second.notification_event_types);
  });

  it.each([
    "mentioned",
    ["mentioned", "unknown_event"],
  ])("falls back only the malformed selection for %j", (notificationEventTypes) => {
    const parsed = LarkInstallationSchema.parse({
      ...baseInstallation,
      notification_event_types: notificationEventTypes,
    });

    expect(parsed.id).toBe(baseInstallation.id);
    expect(parsed.notification_event_types).toEqual(DEFAULT_LARK_NOTIFICATION_EVENTS);
  });

  it("preserves an explicit empty selection", () => {
    const parsed = LarkInstallationSchema.parse({
      ...baseInstallation,
      notification_event_types: [],
    });

    expect(parsed.notification_event_types).toEqual([]);
  });
});

describe("ListLarkInstallationsResponseSchema", () => {
  it("parses a complete valid list", () => {
    const parsed = ListLarkInstallationsResponseSchema.parse({
      installations: [{ ...baseInstallation, notification_event_types: ["mentioned"] }],
      configured: true,
      install_supported: true,
    });

    expect(parsed).toMatchObject({
      configured: true,
      install_supported: true,
      installations: [{ id: baseInstallation.id, notification_event_types: ["mentioned"] }],
    });
  });

  it("filters a malformed installation while preserving valid rows and flags", () => {
    const parsed = ListLarkInstallationsResponseSchema.parse({
      installations: [
        baseInstallation,
        { ...baseInstallation, id: 42 },
        { ...baseInstallation, id: "installation-2", notification_event_types: [] },
      ],
      configured: true,
      install_supported: false,
    });

    expect(parsed.installations.map((installation) => installation.id)).toEqual([
      "installation-1",
      "installation-2",
    ]);
    expect(parsed.installations[1]?.notification_event_types).toEqual([]);
    expect(parsed.configured).toBe(true);
    expect(parsed.install_supported).toBe(false);
  });

  it("uses the defined fallback for a malformed top-level response", () => {
    const first = parseWithFallback(
      null,
      ListLarkInstallationsResponseSchema,
      createEmptyListLarkInstallationsResponse(),
      { endpoint: "GET /api/workspaces/:workspaceId/lark/installations" },
    );
    first.installations.push({ ...baseInstallation, notification_event_types: [] });
    const second = parseWithFallback(
      null,
      ListLarkInstallationsResponseSchema,
      createEmptyListLarkInstallationsResponse(),
      { endpoint: "GET /api/workspaces/:workspaceId/lark/installations" },
    );

    expect(second).toEqual({
      installations: [],
      configured: false,
      notification_event_types: [...DEFAULT_LARK_NOTIFICATION_EVENTS],
    });
    expect(second).not.toBe(first);
    expect(second.installations).not.toBe(first.installations);
  });
});
