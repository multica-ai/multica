import { cloneElement, type ReactElement, type ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import {
  ProductCapabilitiesProvider,
  type ProductCapabilities,
} from "@multica/core/platform";
import { LOCAL_PRODUCT_CAPABILITIES } from "@multica/core/config";

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockOpenModal = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { open: mockOpenModal, modal: null };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({ open: mockOpenModal, modal: null }),
    },
  ),
}));

// Strip Base UI portal/open-state from the DropdownMenu — same approach as
// `delete-workspace-dialog.test.tsx`. Render items as plain buttons / divs so
// queries can find them without simulating portal mount/open transitions.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    render,
    onClick,
  }: {
    children: ReactNode;
    render?: ReactElement<{ onClick?: () => void }>;
    onClick?: () => void;
  }) => {
    // When a `render` prop is provided the real component clones it (typically
    // an <a>) with the children inserted as its content, so the accessible
    // name comes from the children.
    if (render) {
      return cloneElement(render, { onClick }, children);
    }
    return (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    );
  },
}));

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { HelpLauncher } from "./help-launcher";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeCaps(overrides: Partial<ProductCapabilities>): ProductCapabilities {
  return {
    ...LOCAL_PRODUCT_CAPABILITIES,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("HelpLauncher", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("hides Feedback when remote feedback submission is disabled (default local)", () => {
    render(<HelpLauncher />);

    expect(
      screen.queryByRole("button", { name: /feedback/i }),
    ).not.toBeInTheDocument();
    // Docs and Change log are always present.
    expect(screen.getByRole("link", { name: /docs/i })).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /change log/i }),
    ).toBeInTheDocument();
  });

  it("renders Feedback when remote feedback submission is enabled", () => {
    const cloudCaps = makeCaps({
      feedback: {
        ...LOCAL_PRODUCT_CAPABILITIES.feedback,
        allowRemoteSubmission: true,
      },
    });

    render(
      <ProductCapabilitiesProvider capabilities={cloudCaps}>
        <HelpLauncher />
      </ProductCapabilitiesProvider>,
    );

    expect(
      screen.getByRole("button", { name: /feedback/i }),
    ).toBeInTheDocument();
  });
});
