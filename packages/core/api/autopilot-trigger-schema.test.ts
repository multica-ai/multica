// @vitest-environment node
import { expect, it } from "vitest";
import { AutopilotTriggerSchema, FALLBACK_AUTOPILOT_TRIGGER } from "./schemas";
import { parseWithFallback } from "./schema";

it("preserves GitHub admission readback without signing material", () => {
  const result = AutopilotTriggerSchema.parse({
    id: "trigger", autopilot_id: "autopilot", kind: "webhook", enabled: false,
    provider: "github", has_signing_secret: true, signing_secret: "never-return-this",
  });
  expect(result.provider).toBe("github");
  expect(result.has_signing_secret).toBe(true);
  expect(result).not.toHaveProperty("signing_secret");
});

it("degrades missing or malformed readback to disabled and unverified", () => {
  expect(AutopilotTriggerSchema.parse({id:"old",autopilot_id:"ap",kind:"webhook"})).toMatchObject({enabled:false,has_signing_secret:false,provider:null});
  expect(parseWithFallback({enabled:"yes"}, AutopilotTriggerSchema, FALLBACK_AUTOPILOT_TRIGGER, {endpoint:"test"})).toMatchObject({enabled:false,has_signing_secret:false});
});
