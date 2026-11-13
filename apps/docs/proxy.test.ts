// @vitest-environment node

import "next/dist/server/node-environment-baseline";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { describe, expect, it } from "vitest";
import nextConfig from "./next.config.mjs";
import { config } from "./proxy";

describe("docs locale proxy matcher", () => {
  it.each(["/docs/icon.svg", "/docs/icon.svg?icon.1234.svg"])(
    "serves the favicon without a locale rewrite at %s",
    (url) => {
      expect(unstable_doesMiddlewareMatch({ config, nextConfig, url })).toBe(false);
    },
  );

  it.each(["/docs", "/docs/agents", "/docs/zh", "/docs/fr/agents"])(
    "keeps locale routing enabled for %s",
    (url) => {
      expect(unstable_doesMiddlewareMatch({ config, nextConfig, url })).toBe(true);
    },
  );
});
