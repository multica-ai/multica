import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { notificationPreferenceKeys } from "@multica/core/notification-preferences/queries";
import enInbox from "../../locales/en/inbox.json";
import enSettings from "../../locales/en/settings.json";
import { AutoArchivePromptDialog } from "./auto-archive-prompt-dialog";

const { resolve } = vi.hoisted(() => ({ resolve: vi.fn() }));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useResolveAutoArchivePrompt: () => ({ mutate: resolve }),
}));

afterEach(() => {
  cleanup();
  resolve.mockReset();
});

function renderDialog({ prompt, active = true }: { prompt: boolean; active?: boolean }) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  qc.setQueryData(notificationPreferenceKeys.all("ws-a"), {
    workspace_id: "ws-a",
    preferences: {},
    auto_archive_prompt: prompt,
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { inbox: enInbox, settings: enSettings } }}>
        <AutoArchivePromptDialog wsId="ws-a" active={active} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const off = {
  auto_archive_completed: "muted",
  auto_archive_on_agent_trigger: "muted",
  auto_archive_on_comment: "muted",
  auto_archive_on_status: "muted",
  auto_archive_on_assignee: "muted",
  auto_archive_on_priority: "muted",
  auto_archive_on_project: "muted",
  auto_archive_on_dates: "muted",
  auto_archive_on_labels: "muted",
};

describe("AutoArchivePromptDialog", () => {
  it.each([
    { prompt: false, active: true },
    { prompt: true, active: false },
  ])("stays closed unless the question is pending and a notification is open: %j", (props) => {
    renderDialog(props);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("asks once a notification is open, with the recommended choice picked", () => {
    renderDialog({ prompt: true });
    expect(screen.getByRole("dialog", { name: "Archive notifications once you've handled them" })).toBeInTheDocument();
    const choices = screen.getAllByRole("radio").map((radio) => [radio.textContent, radio.getAttribute("aria-checked")]);
    expect(choices).toEqual([
      [
        "When you complete it or trigger an agentRecommendedCompleting an issue or starting an agent archives it; organizing or commenting without waking an agent leaves it in place",
        "true",
      ],
      ["When you complete itOnly completing or cancelling an issue archives it", "false"],
      ["NeverYou archive notifications yourself", "false"],
    ]);
  });

  it("saves the choice for every pending workspace and closes", async () => {
    const user = userEvent.setup();
    renderDialog({ prompt: true });
    await user.click(screen.getByRole("radio", { name: /Only completing or cancelling an issue/ }));
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(resolve).toHaveBeenCalledWith({ ...off, auto_archive_completed: "all" }, expect.anything());
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps what you have when you close it", async () => {
    const user = userEvent.setup();
    renderDialog({ prompt: true });
    await user.keyboard("{Escape}");
    expect(resolve).toHaveBeenCalledWith({}, expect.anything());
  });
});
