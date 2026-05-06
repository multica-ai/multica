import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Strip Base UI Tabs so the portal/animation chrome doesn't interfere with the
// visibility assertions. The slice tests tab existence, not Base UI behavior.
vi.mock("@multica/ui/components/ui/tabs", () => ({
  Tabs: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  TabsList: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  TabsTrigger: ({ children, value }: { children: ReactNode; value: string }) => (
    <button role="tab" data-value={value}>
      {children}
    </button>
  ),
  TabsContent: ({ children, value }: { children: ReactNode; value: string }) => (
    <div role="tabpanel" data-value={value}>
      {children}
    </div>
  ),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Test workspace" }),
}));

vi.mock("./account-tab", () => ({ AccountTab: () => <div>account-tab</div> }));
vi.mock("./appearance-tab", () => ({ AppearanceTab: () => <div>appearance-tab</div> }));
vi.mock("./tokens-tab", () => ({ TokensTab: () => <div>tokens-tab</div> }));
vi.mock("./workspace-tab", () => ({ WorkspaceTab: () => <div>workspace-tab</div> }));
vi.mock("./members-tab", () => ({ MembersTab: () => <div>members-tab</div> }));
vi.mock("./repositories-tab", () => ({ RepositoriesTab: () => <div>repositories-tab</div> }));
vi.mock("./labs-tab", () => ({ LabsTab: () => <div>labs-tab</div> }));
vi.mock("./notifications-tab", () => ({ NotificationsTab: () => <div>notifications-tab</div> }));

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import {
  ProductCapabilitiesProvider,
  type ProductCapabilities,
} from "@multica/core/platform";
import { LOCAL_PRODUCT_CAPABILITIES } from "@multica/core/config";
import { SettingsPage } from "./settings-page";

const fullCapabilities: ProductCapabilities = {
  ...LOCAL_PRODUCT_CAPABILITIES,
  auth: { ...LOCAL_PRODUCT_CAPABILITIES.auth, showApiTokens: true },
  collaboration: {
    ...LOCAL_PRODUCT_CAPABILITIES.collaboration,
    showMembers: true,
  },
};

describe("SettingsPage capability gating", () => {
  it("hides API Tokens and Members tabs under default (local) capabilities", () => {
    render(<SettingsPage />);

    expect(screen.queryByRole("tab", { name: /api tokens/i })).toBeNull();
    expect(screen.queryByRole("tab", { name: /members/i })).toBeNull();

    // Sanity check: other tabs still render.
    expect(screen.getByRole("tab", { name: /profile/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /general/i })).toBeInTheDocument();
  });

  it("shows API Tokens and Members tabs when capabilities allow", () => {
    render(
      <ProductCapabilitiesProvider capabilities={fullCapabilities}>
        <SettingsPage />
      </ProductCapabilitiesProvider>,
    );

    expect(screen.getByRole("tab", { name: /api tokens/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /members/i })).toBeInTheDocument();
  });
});
