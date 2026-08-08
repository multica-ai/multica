import { describe, expect, it, vi } from "vitest";

const sharedPage = vi.hoisted(() => vi.fn(() => null));

vi.mock("@multica/views/extensions", () => ({
  ExtensionsPage: sharedPage,
}));

import ExtensionsRoute from "./page";

describe("web extensions route", () => {
  it("is a thin export of the shared page", () => {
    expect(ExtensionsRoute).toBe(sharedPage);
  });
});
