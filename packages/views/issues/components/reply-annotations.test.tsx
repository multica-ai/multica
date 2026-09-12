import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { renderWithI18n } from "../../test/i18n";
import { ReplyAnnotations } from "./reply-annotations";

const draftKey = "reply:issue:thread" as const;
const annotation = { id: "one", sourceCommentId: "source", sourceActorName: "Emacs", quote: "Source text", note: "My note", start: 0, prefix: "", suffix: "" };

function Fixture({ onEdit = () => true }: { onEdit?: (id: string) => boolean }) {
  const annotations = useCommentDraftStore((s) => s.getAnnotations(draftKey));
  return annotations.length ? <ReplyAnnotations draftKey={draftKey} annotations={annotations} disabled={false} onEditAnnotation={onEdit} /> : null;
}

beforeEach(() => {
  useCommentDraftStore.setState({ drafts: {} });
  useCommentDraftStore.getState().addAnnotation(draftKey, annotation);
});

describe("reply annotation summary", () => {
  it("opens on hover in a portal and returns to the source without expanding the composer", async () => {
    const user = userEvent.setup();
    const onEdit = vi.fn(() => true);
    const { container } = renderWithI18n(<Fixture onEdit={onEdit} />);
    expect(screen.queryByText("Source text")).not.toBeInTheDocument();
    await user.hover(screen.getByRole("button", { name: "1 annotation" }));
    expect(await screen.findByText("Source text")).toBeVisible();
    expect(container).not.toHaveTextContent("Source text");
    const popup = screen.getByRole("dialog", { name: "1 annotation" });
    await user.hover(popup);
    expect(screen.getByText("My note")).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Preview reply" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Edit annotation 1" }));
    expect(onEdit).toHaveBeenCalledWith("one");
    await waitFor(() => expect(screen.queryByText("Source text")).not.toBeInTheDocument());
  });

  it("closes after the pointer leaves the trigger and popup", async () => {
    const user = userEvent.setup();
    renderWithI18n(<Fixture />);
    const trigger = screen.getByRole("button", { name: "1 annotation" });
    await user.hover(trigger);
    await screen.findByText("Source text");
    await user.unhover(trigger);
    await waitFor(() => expect(screen.queryByText("Source text")).not.toBeInTheDocument());
  });

  it("supports keyboard opening, Escape dismissal and focus return", async () => {
    const user = userEvent.setup();
    renderWithI18n(<Fixture />);
    await user.tab();
    const trigger = screen.getByRole("button", { name: "1 annotation" });
    expect(trigger).toHaveFocus();
    await user.keyboard("{Enter}");
    await screen.findByText("Source text");
    await user.tab();
    expect(screen.getByRole("button", { name: "Edit annotation 1" })).toHaveFocus();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByText("Source text")).not.toBeInTheDocument());
    expect(trigger).toHaveFocus();
  });

  it("does not steal focus back when a quote opens its source editor", async () => {
    const user = userEvent.setup();
    renderWithI18n(<>
      <textarea aria-label="Source note editor" />
      <Fixture onEdit={() => {
        screen.getByRole("textbox", { name: "Source note editor" }).focus();
        return true;
      }} />
    </>);
    await user.click(screen.getByRole("button", { name: "1 annotation" }));
    await user.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    await waitFor(() => expect(screen.queryByText("Source text")).not.toBeInTheDocument());
    expect(screen.getByRole("textbox", { name: "Source note editor" })).toHaveFocus();
  });

  it("keeps an unavailable quote and note readable and removable", async () => {
    renderWithI18n(<Fixture onEdit={() => false} />);
    fireEvent.click(screen.getByRole("button", { name: "1 annotation" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit annotation 1" }));
    expect(screen.getByRole("status")).toHaveTextContent("Your saved quote is kept");
    expect(screen.getByText("My note")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Remove annotation 1" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "1 annotation" })).not.toBeInTheDocument());
    expect(useCommentDraftStore.getState().getAnnotations(draftKey)).toHaveLength(0);
  });
});
