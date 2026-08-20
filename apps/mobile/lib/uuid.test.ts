import { beforeEach, describe, expect, it, vi } from "vitest";

const { expoRandomUUID } = vi.hoisted(() => ({
  expoRandomUUID: vi.fn(),
}));
vi.mock("expo-crypto", () => ({ randomUUID: expoRandomUUID }));

import { randomUUID } from "./uuid";

describe("randomUUID", () => {
  beforeEach(() => {
    expoRandomUUID.mockReset();
  });

  it("uses Expo Crypto's native UUID generator", () => {
    expoRandomUUID.mockReturnValue("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11");
    expect(randomUUID()).toBe("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11");
    expect(expoRandomUUID).toHaveBeenCalledOnce();
  });
});
