import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  patchOnboarding: vi.fn(),
  setUser: vi.fn(),
}));

vi.mock("../api", () => ({ api: { patchOnboarding: mocks.patchOnboarding } }));
vi.mock("../auth", () => ({
  useAuthStore: { getState: () => ({ setUser: mocks.setUser }) },
}));
vi.mock("../analytics", () => ({ setPersonProperties: vi.fn() }));

import type { User } from "../types";
import { recordOnboardingStarted } from "./store";

function user(questionnaire: Record<string, unknown>) {
  return { id: "u1", onboarding_questionnaire: questionnaire } as unknown as User;
}

describe("recordOnboardingStarted", () => {
  beforeEach(() => {
    mocks.patchOnboarding.mockReset().mockResolvedValue(user({ version: 2 }));
    mocks.setUser.mockReset();
  });

  it("writes the empty questionnaire for someone who never started", async () => {
    await recordOnboardingStarted(user({}));

    expect(mocks.patchOnboarding).toHaveBeenCalledWith({ questionnaire: { version: 2 } });
    expect(mocks.setUser).toHaveBeenCalledWith(user({ version: 2 }));
  });

  it("leaves an existing questionnaire alone", async () => {
    await recordOnboardingStarted(user({ version: 2 }));
    await recordOnboardingStarted(user({ role: "engineer", use_case: ["ship_code"], version: 2 }));

    expect(mocks.patchOnboarding).not.toHaveBeenCalled();
  });
});
