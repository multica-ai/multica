// @vitest-environment node

import { Image, type ImageProps } from "fumadocs-core/framework";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { DocsFrameworkProvider } from "./i18n-frame-provider";

const screenshot = {
  src: "/docs/_next/static/media/screenshot.webp",
  width: 1440,
  height: 900,
};

describe("DocsFrameworkProvider images", () => {
  it.each([
    ["static image data", screenshot],
    ["static import module", { default: screenshot }],
    ["URL with dimensions", screenshot.src],
  ] satisfies [string, ImageProps["src"]][])(
    "renders a usable image from %s",
    (_label, src) => {
      const html = renderToStaticMarkup(
        createElement(
          DocsFrameworkProvider,
          null,
          createElement(Image, {
            src,
            alt: "Docs screenshot",
            ...(typeof src === "string" ? { width: 1440, height: 900 } : {}),
          }),
        ),
      );
      const image = html.match(/<img\b[^>]*>/)?.[0];

      expect(image).toContain('alt="Docs screenshot"');
      expect(image).toMatch(/\ssrc="[^"]+"/);
      expect(image).toContain(encodeURIComponent(screenshot.src));
      expect(image).toContain('width="1440"');
      expect(image).toContain('height="900"');
    },
  );
});
