// @vitest-environment node
import { describe, expect, it } from "vitest";
import { UserSchema } from "../api/schemas";
import { PlatformSettingsSchema, PlatformUserPageSchema } from "./schema";

describe("platform administration schemas", () => {
  it("preserves explicit false and empty allowlists", () => {
    const settings = [
      { key: "allow_workspace_creation", value: false, source: "platform", available: true },
      { key: "signup_allowed_emails", value: [], source: "platform", available: true },
    ];
    expect(PlatformSettingsSchema.parse(settings)).toEqual(settings);
    expect(PlatformSettingsSchema.safeParse([{ ...settings[0], value: "false" }]).success).toBe(false);
  });

  it("rejects user records with malformed membership state", () => {
    expect(PlatformUserPageSchema.safeParse({
      items: [{ id: "u", name: "User", email: "u@example.test", has_workspace: "false" }],
      next: "",
    }).success).toBe(false);
  });

  it("keeps older account responses and malformed admin markers compatible", () => {
    expect(UserSchema.parse({ id: "u" }).can_create_workspace).toBeUndefined();
    expect(UserSchema.parse({ id: "u", can_create_workspace: false }).can_create_workspace).toBe(false);
    expect(UserSchema.parse({ id: "u", platform_admin: "true" }).platform_admin).toBeUndefined();
  });
});
