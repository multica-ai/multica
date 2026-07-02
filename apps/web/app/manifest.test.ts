import { describe, it, expect } from "vitest";
import manifest from "./manifest";

// These assertions encode Multica's PWA install contract. Regressing them puts
// the address-bar install affordance at risk; browsers also layer engagement
// heuristics on top, so passing here is necessary but not sufficient on its own.
describe("web app manifest", () => {
  const m = manifest();

  it("declares the core fields browsers require for installability", () => {
    expect(m.name).toBeTruthy();
    expect(m.short_name).toBeTruthy();
    expect(m.start_url).toBe("/");
    expect(m.scope).toBe("/");
    expect(m.display).toBe("standalone");
    // Pinned id keeps the installed identity stable across start_url changes.
    expect(m.id).toBe("/");
  });

  it("ships at least 192px and 512px PNG icons under /icons", () => {
    const icons = m.icons ?? [];
    const sizes = icons.map((i) => i.sizes);
    expect(sizes).toContain("192x192");
    expect(sizes).toContain("512x512");
    for (const icon of icons) {
      expect(icon.type).toBe("image/png");
      expect(icon.src.startsWith("/icons/")).toBe(true);
    }
  });

  it("provides at least one maskable icon for adaptive launchers", () => {
    const maskable = (m.icons ?? []).filter((i) => i.purpose === "maskable");
    expect(maskable.length).toBeGreaterThanOrEqual(1);
    // A maskable icon must also exist at 512px so launchers can render crisply.
    expect(maskable.some((i) => i.sizes === "512x512")).toBe(true);
  });
});
