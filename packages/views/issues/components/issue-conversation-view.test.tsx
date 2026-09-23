import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { IssueConversationView } from "./issue-conversation-view";
import { useIssueConversationView } from "./issue-conversation-view-context";

vi.mock("./issue-conversation", () => ({
  IssueConversation: ({ inline, initialRunId, onClose }: { inline: boolean; initialRunId: string; onClose: () => void }) =>
    <section data-testid="inline-conversation" data-inline={inline}>
      {initialRunId}<button onClick={onClose}>Back to issue</button>
    </section>,
}));
afterEach(cleanup);

function IssueBody() {
  const view = useIssueConversationView();
  return <div><input aria-label="Issue draft" defaultValue="" />
    <button onClick={() => view?.open("selected-run")}>Open conversation</button>
  </div>;
}

it("switches locally to the selected run and back without unmounting the issue", () => {
  render(<div>
    <IssueConversationView issueId="issue" tasks={[]} header={<header>Issue toolbar</header>}><IssueBody /></IssueConversationView>
    <aside>Issue properties</aside>
  </div>);
  const draft = screen.getByRole("textbox");
  fireEvent.change(draft, { target: { value: "unsaved draft" } });
  fireEvent.click(screen.getByRole("button", { name: "Open conversation" }));
  expect(screen.getByTestId("inline-conversation")).toHaveAttribute("data-inline", "true");
  expect(screen.getByTestId("inline-conversation")).toHaveTextContent("selected-run");
  expect(draft).not.toBeVisible();
  expect(screen.getByText("Issue toolbar")).toBeVisible();
  expect(screen.getByText("Issue properties")).toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Back to issue" }));
  expect(screen.getByRole("textbox")).toBe(draft);
  expect(draft).toHaveValue("unsaved draft");
  expect(draft).toBeVisible();
});
