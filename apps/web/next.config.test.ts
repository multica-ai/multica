// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { hasLocalMatch } from "next/dist/shared/lib/match-local-pattern";

vi.mock("dotenv", () => ({ config: vi.fn() }));
vi.mock("fumadocs-mdx/next", () => ({
  createMDX: () => (config: unknown) => config,
}));

import nextConfig from "./next.config";

describe("image optimization paths", () => {
  it.each([
    "/uploads/workspaces/workspace/attachment.png",
    "/api/attachments/attachment/download",
    "/api/attachments/attachment/signed-download?exp=1&sig=signature",
    "/images/hero.png?url=/uploads/workspaces/workspace/attachment.png",
  ])("rejects private or unexpected sources: %s", (url) => {
    expect(hasLocalMatch(nextConfig.images?.localPatterns, url)).toBe(false);
  });

  it.each(["/images/hero.png", "/usecases/team/screenshot.webp"])(
    "keeps landing images available: %s",
    (url) => {
      expect(hasLocalMatch(nextConfig.images?.localPatterns, url)).toBe(true);
    },
  );
});
