import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WorkspaceAvatar } from "./workspace-avatar";

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "http://127.0.0.1:8080" },
}));

afterEach(cleanup);

const LOGO = "https://cdn.example.com/logo.png";

function avatarOf(ui: React.ReactElement) {
  return render(ui).container.firstElementChild as HTMLElement;
}

describe("WorkspaceAvatar shape", () => {
  for (const [path, avatarUrl] of [["logo", LOGO], ["letter", null]] as const) {
    it(`takes the theme's avatar radius (${path})`, () => {
      const avatar = avatarOf(<WorkspaceAvatar name="Acme" avatarUrl={avatarUrl} />);
      expect(avatar.classList).toContain("rounded-avatar");
      expect(avatar.classList).not.toContain("rounded-full");
    });

    it(`keeps a fallback radius under the theme's (${path})`, () => {
      const avatar = avatarOf(<WorkspaceAvatar name="Acme" avatarUrl={avatarUrl} radius="lg" />);
      expect(avatar.classList).toContain("rounded-[var(--avatar-radius,var(--radius-lg))]");
      expect(avatar.classList).not.toContain("rounded-avatar");
    });

    it(`ignores a call-site radius (${path})`, () => {
      const avatar = avatarOf(
        <WorkspaceAvatar name="Acme" avatarUrl={avatarUrl} className="rounded-md" />,
      );
      expect(avatar.classList).toContain("rounded-avatar");
      expect(avatar.classList).not.toContain("rounded-md");
    });
  }
});
