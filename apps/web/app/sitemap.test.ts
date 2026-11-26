// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isLiveRoute, siteRoutes } from "@/features/landing/site/sitemap";
import sitemap from "./sitemap";

describe("sitemap.xml", () => {
  const entries = sitemap();
  const urls = entries.map((entry) => entry.url);

  it("lists every released marketing route, including /download", () => {
    const live = siteRoutes().filter(isLiveRoute);
    expect(urls).toEqual(live.map((route) => (route === "/" ? "https://www.multica.ai" : `https://www.multica.ai${route}`)));
    expect(urls).toContain("https://www.multica.ai/download");
    expect(urls).not.toContain("https://www.multica.ai/usecases");
  });

  it("dates only the pages that change with each release", () => {
    const dated = entries.filter((entry) => entry.lastModified).map((entry) => entry.url);
    expect(dated).toEqual(["https://www.multica.ai", "https://www.multica.ai/changelog"]);
  });

  it("declares every language version", () => {
    const download = entries.find((entry) => entry.url.endsWith("/download"));
    expect(download?.alternates?.languages).toMatchObject({
      en: "https://www.multica.ai/download",
      "zh-Hans": "https://www.multica.ai/zh/download",
      "x-default": "https://www.multica.ai/download",
    });
  });
});
