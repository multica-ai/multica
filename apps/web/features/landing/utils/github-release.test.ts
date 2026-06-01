import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchLatestRelease } from "./github-release";

const BASE = "https://multica.lilithgames.com/api/downloads";

const MAC_YML = `version: 0.2.53
files:
  - url: multica-desktop-0.2.53-mac-arm64.zip
    sha512: aaa
    size: 1
  - url: multica-desktop-0.2.53-mac-arm64.dmg
    sha512: bbb
    size: 2
path: multica-desktop-0.2.53-mac-arm64.zip
sha512: aaa
releaseDate: '2026-06-01T08:17:40.274Z'
`;

const WIN_YML = `version: 0.2.53
files:
  - url: multica-desktop-0.2.53-windows-x64.exe
    sha512: ccc
    size: 3
path: multica-desktop-0.2.53-windows-x64.exe
releaseDate: '2026-06-01T08:17:40.274Z'
`;

const LINUX_YML = `version: 0.2.53
files:
  - url: multica-desktop-0.2.53-linux-x86_64.AppImage
    sha512: ddd
    size: 4
  - url: multica-desktop-0.2.53-linux-amd64.deb
    sha512: eee
    size: 5
  - url: multica-desktop-0.2.53-linux-x86_64.rpm
    sha512: fff
    size: 6
releaseDate: '2026-06-01T08:17:40.274Z'
`;

// map: manifest filename -> yml body (200) or status code (failure).
function mockManifests(map: Record<string, string | number>) {
  const fetchMock = vi.fn(async (url: string | URL) => {
    const name = String(url).replace(`${BASE}/`, "");
    const entry = map[name];
    if (typeof entry === "string") {
      return new Response(entry, { status: 200 });
    }
    return new Response("not found", {
      status: typeof entry === "number" ? entry : 404,
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("fetchLatestRelease", () => {
  it("builds OSS installer URLs from the latest-*.yml manifests", async () => {
    mockManifests({
      "latest-mac.yml": MAC_YML,
      "latest.yml": WIN_YML,
      "latest-linux.yml": LINUX_YML,
    });

    const r = await fetchLatestRelease();
    expect(r.version).toBe("0.2.53");
    expect(r.htmlUrl).toBeNull();
    expect(r.assets.macArm64Zip).toBe(
      `${BASE}/multica-desktop-0.2.53-mac-arm64.zip`,
    );
    expect(r.assets.macArm64Dmg).toBe(
      `${BASE}/multica-desktop-0.2.53-mac-arm64.dmg`,
    );
    expect(r.assets.winX64Exe).toBe(
      `${BASE}/multica-desktop-0.2.53-windows-x64.exe`,
    );
    expect(r.assets.linuxAmd64AppImage).toBe(
      `${BASE}/multica-desktop-0.2.53-linux-x86_64.AppImage`,
    );
    expect(r.assets.linuxAmd64Deb).toBe(
      `${BASE}/multica-desktop-0.2.53-linux-amd64.deb`,
    );
    expect(r.assets.linuxAmd64Rpm).toBe(
      `${BASE}/multica-desktop-0.2.53-linux-x86_64.rpm`,
    );
  });

  it("drops a platform whose manifest is missing but keeps the rest", async () => {
    mockManifests({
      "latest-mac.yml": MAC_YML,
      "latest.yml": 404,
      "latest-linux.yml": 404,
    });

    const r = await fetchLatestRelease();
    expect(r.version).toBe("0.2.53");
    expect(r.assets.macArm64Dmg).toBe(
      `${BASE}/multica-desktop-0.2.53-mac-arm64.dmg`,
    );
    expect(r.assets.winX64Exe).toBeUndefined();
    expect(r.assets.linuxAmd64AppImage).toBeUndefined();
  });

  it("degrades to an empty release when every manifest fails", async () => {
    mockManifests({
      "latest-mac.yml": 503,
      "latest.yml": 503,
      "latest-linux.yml": 503,
    });

    const r = await fetchLatestRelease();
    expect(r).toEqual({
      version: null,
      publishedAt: null,
      htmlUrl: null,
      assets: {},
    });
  });
});
