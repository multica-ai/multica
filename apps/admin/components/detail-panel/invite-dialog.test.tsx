// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { InviteDialog } from "./invite-dialog";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

function renderDialog(overrides: Partial<React.ComponentProps<typeof InviteDialog>> = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const onOpenChange = vi.fn();
  render(
    <QueryClientProvider client={queryClient}>
      <InviteDialog
        workspaceId="ws-1"
        open
        onOpenChange={onOpenChange}
        existingMemberEmails={["existing@example.com"]}
        pendingInvitationEmails={["invited@example.com"]}
        {...overrides}
      />
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

describe("InviteDialog", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it("disables submit until a valid, not-yet-a-member email is entered", async () => {
    const user = userEvent.setup();
    renderDialog();
    const submit = screen.getByRole("button", { name: /send invite/i });
    expect(submit).toBeDisabled();

    await user.type(screen.getByLabelText(/email/i), "new@example.com");
    expect(submit).toBeEnabled();
  });

  it("shows a client-side error and keeps submit disabled for an already-existing member, without any network call", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    renderDialog();

    await user.type(screen.getByLabelText(/email/i), "existing@example.com");

    expect(screen.getByText(/already a member/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /send invite/i })).toBeDisabled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows a client-side error for an already-pending invitation", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText(/email/i), "invited@example.com");

    expect(screen.getByText(/already has a pending invitation/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /send invite/i })).toBeDisabled();
  });

  it("submits, toasts success, and closes the dialog on a successful invite", async () => {
    const user = userEvent.setup();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: "inv-1", invitee_email: "new@example.com" }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const { onOpenChange } = renderDialog();

    await user.type(screen.getByLabelText(/email/i), "new@example.com");
    await user.click(screen.getByRole("button", { name: /send invite/i }));

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith(expect.stringContaining("new@example.com")));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("toasts the server's error message on a race-condition failure, and does not close the dialog", async () => {
    const user = userEvent.setup();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "user is already a member" }), {
          status: 409,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const { onOpenChange } = renderDialog();

    await user.type(screen.getByLabelText(/email/i), "new@example.com");
    await user.click(screen.getByRole("button", { name: /send invite/i }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("user is already a member"));
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});
