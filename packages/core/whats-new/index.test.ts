import { describe, expect, it } from "vitest";
import { parseVersion, unreadReleases, visibleReleases } from "./index";

const releases = [
  { version: "0.7.0", date: "2026-10-20" },
  { version: "0.7.10", date: "2026-11-02" },
  { version: "0.7.2", date: "2026-10-30" },
  { version: "0.7.9", date: "2026-11-01" },
];

const versions = (rs: { version: string }[]) => rs.map((r) => r.version);

describe("parseVersion", () => {
  it("accepts a leading v and build suffixes", () => {
    expect(parseVersion("v0.7.2-12-gabc")).toEqual([0, 7, 2]);
    expect(parseVersion("1.2.3")).toEqual([1, 2, 3]);
  });

  it("rejects empty and partial versions", () => {
    expect(parseVersion("")).toBeNull();
    expect(parseVersion("0.7")).toBeNull();
    expect(parseVersion(null)).toBeNull();
  });
});

describe("visibleReleases", () => {
  it("sorts newest first numerically", () => {
    expect(versions(visibleReleases(releases, ""))).toEqual(["0.7.10", "0.7.9", "0.7.2", "0.7.0"]);
  });

  it("caps at the connected server version", () => {
    expect(versions(visibleReleases(releases, "v0.7.2"))).toEqual(["0.7.2", "0.7.0"]);
  });

  it("applies no cap when the server version is unparseable", () => {
    expect(visibleReleases(releases, "dev")).toHaveLength(4);
  });
});

describe("unreadReleases", () => {
  const visible = visibleReleases(releases, "");

  it("returns nothing when the server cannot track the marker", () => {
    expect(unreadReleases(visible, { created_at: "2026-01-01T00:00:00Z" })).toEqual([]);
    expect(unreadReleases(visible, null)).toEqual([]);
  });

  it("returns releases newer than the seen marker", () => {
    const user = { whats_new_seen_version: "0.7.2", created_at: "2026-01-01T00:00:00Z" };
    expect(versions(unreadReleases(visible, user))).toEqual(["0.7.10", "0.7.9"]);
  });

  it("returns nothing once the newest release is seen", () => {
    const user = { whats_new_seen_version: "0.7.10", created_at: "2026-01-01T00:00:00Z" };
    expect(unreadReleases(visible, user)).toEqual([]);
  });

  it("counts only releases after signup before the first dismissal", () => {
    const user = { whats_new_seen_version: null, created_at: "2026-10-30T09:00:00Z" };
    expect(versions(unreadReleases(visible, user))).toEqual(["0.7.10", "0.7.9"]);
  });
});
