import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render as rtlRender, screen, type RenderOptions } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAutopilots from "../../locales/en/autopilots.json";

const TEST_RESOURCES = {
  en: { common: enCommon, autopilots: enAutopilots },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function render(ui: React.ReactElement, options?: RenderOptions) {
  return rtlRender(ui, { wrapper: I18nWrapper, ...options });
}

// The shared Dialog is a Base UI portal that's awkward to test — strip it to
// simple pass-through wrappers. The button state lives in the dialog body.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

// A delete that never settles — the exact condition FIR-4359 hit in production,
// where the request hung for >15s on missing autopilot_run FK indexes.
const mutateAsync = vi.fn(() => new Promise<void>(() => {}));

vi.mock("@multica/core/autopilots", () => ({
  useDeleteAutopilot: () => ({ mutateAsync }),
  useUpdateAutopilot: () => ({ mutateAsync: vi.fn() }),
}));

import { DeleteAutopilotsDialog } from "./autopilot-list-actions";

const ROW = { id: "ap-1", title: "Nightly sync" } as never;

describe("DeleteAutopilotsDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps Cancel usable while a delete is in flight", async () => {
    const user = userEvent.setup();
    const onOpenChange = vi.fn();

    render(
      <DeleteAutopilotsDialog rows={[ROW]} open onOpenChange={onOpenChange} />,
    );

    await user.click(screen.getByRole("button", { name: "Delete permanently" }));

    // Confirm is locked so the delete cannot be double-fired...
    expect(await screen.findByRole("button", { name: /Deleting/ })).toBeDisabled();

    // ...but Cancel must stay the way out of a request that never returns.
    const cancel = screen.getByRole("button", { name: "Cancel" });
    expect(cancel).toBeEnabled();

    await user.click(cancel);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
