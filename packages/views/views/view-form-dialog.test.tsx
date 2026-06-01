import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render as rtlRender, screen, type RenderOptions } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";

const TEST_RESOURCES = { en: { common: enCommon } };

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

// Base UI Dialog is a portal that's awkward to drive in jsdom — strip it to
// pass-through wrappers. The form logic under test lives in our body, not in
// Base UI. (Same approach as delete-workspace-dialog.test.)
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { ViewFormDialog } from "./view-form-dialog";

describe("ViewFormDialog", () => {
  beforeEach(() => vi.clearAllMocks());

  it("disables submit until a non-empty name is entered", async () => {
    const user = userEvent.setup();
    render(
      <ViewFormDialog open mode="create" onSubmit={vi.fn()} onOpenChange={vi.fn()} />,
    );
    expect(screen.getByRole("button", { name: "Create" })).toBeDisabled();
    await user.type(screen.getByRole("textbox"), "My view");
    expect(screen.getByRole("button", { name: "Create" })).toBeEnabled();
  });

  it("submits trimmed name with shared=false by default (create)", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(
      <ViewFormDialog open mode="create" onSubmit={onSubmit} onOpenChange={vi.fn()} />,
    );
    await user.type(screen.getByRole("textbox"), "  Urgent  ");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(onSubmit).toHaveBeenCalledWith({ name: "Urgent", shared: false });
  });

  it("collects the shared toggle on create", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(
      <ViewFormDialog open mode="create" onSubmit={onSubmit} onOpenChange={vi.fn()} />,
    );
    await user.type(screen.getByRole("textbox"), "Team view");
    await user.click(screen.getByRole("switch"));
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(onSubmit).toHaveBeenCalledWith({ name: "Team view", shared: true });
  });

  it("hides the shared toggle in rename mode and prefills the name", () => {
    render(
      <ViewFormDialog
        open
        mode="rename"
        initialName="Old name"
        onSubmit={vi.fn()}
        onOpenChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox")).toHaveValue("Old name");
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("submits on Enter when the name is valid", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(
      <ViewFormDialog open mode="rename" onSubmit={onSubmit} onOpenChange={vi.fn()} />,
    );
    await user.type(screen.getByRole("textbox"), "Renamed{Enter}");
    expect(onSubmit).toHaveBeenCalledWith({ name: "Renamed", shared: false });
  });

  it("resets local state when reopened so a cancelled edit doesn't leak", () => {
    const { rerender } = render(
      <ViewFormDialog
        open
        mode="rename"
        initialName="First"
        onSubmit={vi.fn()}
        onOpenChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox")).toHaveValue("First");
    rerender(
      <ViewFormDialog
        open={false}
        mode="rename"
        initialName="First"
        onSubmit={vi.fn()}
        onOpenChange={vi.fn()}
      />,
    );
    rerender(
      <ViewFormDialog
        open
        mode="rename"
        initialName="Second"
        onSubmit={vi.fn()}
        onOpenChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox")).toHaveValue("Second");
  });
});
