import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
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
  it("shows a compact count and returns to the source instead of creating a second editor", () => {
    const onEdit = vi.fn(() => true);
    renderWithI18n(<Fixture onEdit={onEdit} />);
    expect(screen.queryByText("Source text")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "1 annotation" }));
    expect(screen.getByText("Source text")).toBeVisible();
    expect(screen.getByText("My note")).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Preview reply" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Edit annotation 1" }));
    expect(onEdit).toHaveBeenCalledWith("one");
    expect(screen.queryByText("Source text")).not.toBeInTheDocument();
  });

  it("keeps an unavailable quote and note readable and removable", async () => {
    renderWithI18n(<Fixture onEdit={() => false} />);
    fireEvent.click(screen.getByRole("button", { name: "1 annotation" }));
    fireEvent.click(screen.getByRole("button", { name: "Edit annotation 1" }));
    expect(screen.getByRole("status")).toHaveTextContent("Your saved quote is kept");
    expect(screen.getByText("My note")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Remove annotation 1" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "1 annotation" })).not.toBeInTheDocument());
    expect(useCommentDraftStore.getState().getAnnotations(draftKey)).toHaveLength(0);
  });
});
