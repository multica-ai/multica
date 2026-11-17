import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssueSubscriberDelivery } from "@multica/core/types";
import enIssues from "../../locales/en/issues.json";
import { KeyUpdatesSubscriptionBadge } from "./key-updates-subscription-badge";

const push = vi.fn();
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push }) }));

afterEach(() => {
  cleanup();
  push.mockReset();
});

function renderBadge({
  delivery,
  onFollowAll = vi.fn(),
}: {
  delivery: IssueSubscriberDelivery | undefined;
  onFollowAll?: () => void;
}) {
  render(
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <KeyUpdatesSubscriptionBadge delivery={delivery} pending={false} onFollowAll={onFollowAll} />
    </I18nProvider>,
  );
}

describe("KeyUpdatesSubscriptionBadge", () => {
  it("offers all activity when the server delivers key updates only", async () => {
    const onFollowAll = vi.fn();
    renderBadge({ delivery: "key_updates", onFollowAll });
    fireEvent.click(screen.getByRole("button", { name: "Key updates only" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Get all activity" }));
    expect(onFollowAll).toHaveBeenCalledOnce();
  });

  it("links to the default in settings", async () => {
    renderBadge({ delivery: "key_updates" });
    fireEvent.click(screen.getByRole("button", { name: "Key updates only" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Change the default" }));
    expect(push).toHaveBeenCalledWith("/acme/settings?tab=notifications&section=others_issues");
  });

  // undefined is a server that predates `delivery`, or a row patched locally
  // and not yet refetched: no tier is known, so no badge.
  it.each([
    { delivery: "all" as const },
    { delivery: "delegated" as const },
    { delivery: undefined },
  ])("stays hidden for delivery $delivery", ({ delivery }) => {
    renderBadge({ delivery });
    expect(screen.queryByRole("button", { name: "Key updates only" })).toBeNull();
  });

  // Clickable chips use rounded-md (packages/ui/docs/button.md).
  it("uses the clickable chip radius", () => {
    renderBadge({ delivery: "key_updates" });
    const trigger = screen.getByRole("button", { name: "Key updates only" });
    expect(trigger.className).toContain("rounded-md");
    expect(trigger.className).not.toContain("rounded-full");
  });
});
