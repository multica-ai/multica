import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { InboxItem } from "@multica/core/types";
import enInbox from "../../locales/en/inbox.json";
import { InboxDeleteDialog } from "./inbox-delete-dialog";
import type { InboxView } from "./inbox-view";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

const entry = { id: "notification-1", title: "Create with agent failed", issue_id: null } as InboxItem;
function setup(remove = vi.fn(async () => ({ count: 1 })), issueId: string | null = null, view: InboxView = "inbox") {
  setApiInstance({ deleteInbox: remove } as unknown as ApiClient);
  const onClose = vi.fn();
  const onDeleted = vi.fn();
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { inbox: enInbox } }}>
        <InboxDeleteDialog item={{ ...entry, issue_id: issueId }} view={view} onClose={onClose} onDeleted={onDeleted} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { remove, onClose, onDeleted };
}

describe("inbox deletion confirmation", () => {
  it("cancels without deleting", () => {
    const { remove, onClose, onDeleted } = setup();
    expect(screen.getByRole("alertdialog")).toHaveAccessibleName("Delete notification?");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledOnce();
    expect(remove).not.toHaveBeenCalled();
    expect(onDeleted).not.toHaveBeenCalled();
  });

  it("keeps the dialog and selection until the server confirms, blocking repeated clicks", async () => {
    let resolve!: (value: { count: number }) => void;
    const remove = vi.fn(() => new Promise<{ count: number }>((done) => { resolve = done; }));
    const { onClose, onDeleted } = setup(remove);
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(remove).toHaveBeenCalledWith("notification-1"));
    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(onClose).not.toHaveBeenCalled();
    expect(onDeleted).not.toHaveBeenCalled();
    await act(async () => resolve({ count: 1 }));
    await waitFor(() => expect(onDeleted).toHaveBeenCalledWith("notification-1"));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("shows a retryable error without closing or moving selection", async () => {
    const remove = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({ count: 1 });
    const { onClose, onDeleted } = setup(remove);
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Failed to delete notification. Try again.");
    expect(onClose).not.toHaveBeenCalled();
    expect(onDeleted).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onDeleted).toHaveBeenCalledWith("notification-1"));
    expect(remove).toHaveBeenCalledTimes(2);
  });

  it("explains that deleting active issue notifications makes old archived history visible again", () => {
    setup(undefined, "issue-1", "inbox");
    expect(screen.getByRole("alertdialog")).toHaveTextContent("Previously archived notifications remain and will be visible again in Archived.");
  });

  it("explains the archived group scope without promising to retain the deleted history", () => {
    setup(undefined, "issue-1", "archived");
    expect(screen.getByText(enInbox.delete_dialog.issue_description)).toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).not.toHaveTextContent("Previously archived notifications");
  });

  it("does not mention archived history for an issue-less active notification", () => {
    setup();
    expect(screen.getByText(enInbox.delete_dialog.description)).toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).not.toHaveTextContent("Previously archived notifications");
  });
});
