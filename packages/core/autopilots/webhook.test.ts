import { describe, expect, it } from "vitest";
import { buildAutopilotWebhookUrl, maskAutopilotWebhookUrl } from "./webhook";
import type { AutopilotTrigger } from "../types";

const baseTrigger: AutopilotTrigger = {
  id: "t1",
  autopilot_id: "a1",
  kind: "webhook",
  enabled: true,
  cron_expression: null,
  timezone: null,
  next_run_at: null,
  webhook_token: "awt_abc",
  webhook_path: "/api/webhooks/autopilots/awt_abc",
  webhook_url: null,
  label: null,
  last_fired_at: null,
  created_at: "",
  updated_at: "",
};

describe("buildAutopilotWebhookUrl", () => {
  it("returns the server-provided webhook_url verbatim when present", () => {
    expect(
      buildAutopilotWebhookUrl({
        trigger: { ...baseTrigger, webhook_url: "https://custom.example/api/webhooks/autopilots/awt_abc" },
      }),
    ).toBe("https://custom.example/api/webhooks/autopilots/awt_abc");
  });

  it("composes from apiBaseUrl + webhook_path", () => {
    expect(
      buildAutopilotWebhookUrl({ trigger: baseTrigger, apiBaseUrl: "https://api.example" }),
    ).toBe("https://api.example/api/webhooks/autopilots/awt_abc");
  });

  it("strips trailing slash on apiBaseUrl", () => {
    expect(
      buildAutopilotWebhookUrl({ trigger: baseTrigger, apiBaseUrl: "https://api.example/" }),
    ).toBe("https://api.example/api/webhooks/autopilots/awt_abc");
  });

  it("falls back to currentOrigin when apiBaseUrl is empty", () => {
    expect(
      buildAutopilotWebhookUrl({
        trigger: baseTrigger,
        apiBaseUrl: "",
        currentOrigin: "https://app.example",
      }),
    ).toBe("https://app.example/api/webhooks/autopilots/awt_abc");
  });

  it("composes from token when webhook_path is missing", () => {
    expect(
      buildAutopilotWebhookUrl({
        trigger: { ...baseTrigger, webhook_path: null },
        apiBaseUrl: "https://api.example",
      }),
    ).toBe("https://api.example/api/webhooks/autopilots/awt_abc");
  });

  it("returns null for non-webhook trigger", () => {
    expect(
      buildAutopilotWebhookUrl({
        trigger: { ...baseTrigger, kind: "schedule", webhook_token: null, webhook_path: null },
      }),
    ).toBeNull();
  });

  it("returns relative path when no base or origin available", () => {
    expect(buildAutopilotWebhookUrl({ trigger: baseTrigger })).toBe("/api/webhooks/autopilots/awt_abc");
  });
});

describe("maskAutopilotWebhookUrl", () => {
  it("masks the token segment and keeps the rest readable", () => {
    const masked = maskAutopilotWebhookUrl("https://api.example/api/webhooks/autopilots/awt_abc");
    expect(masked).toBe("https://api.example/api/webhooks/autopilots/••••••••••••");
    expect(masked).not.toContain("awt_abc");
  });

  it("masks the token on a relative path", () => {
    expect(maskAutopilotWebhookUrl("/api/webhooks/autopilots/awt_abc")).toBe(
      "/api/webhooks/autopilots/••••••••••••",
    );
  });

  it("uses a fixed-width mask so the token length never leaks", () => {
    const short = maskAutopilotWebhookUrl("https://api.example/hooks/a");
    const long = maskAutopilotWebhookUrl("https://api.example/hooks/" + "z".repeat(64));
    expect(short).toBe(long);
  });

  it("masks the whole value when there is no separable last segment", () => {
    expect(maskAutopilotWebhookUrl("awt_abc")).toBe("••••••••••••");
    expect(maskAutopilotWebhookUrl("https://api.example/hooks/")).toBe("••••••••••••");
  });
});
