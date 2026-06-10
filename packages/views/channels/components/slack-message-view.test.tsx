import React from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TimelineEntry } from "@multica/core/types";

const mockOpenReminder = vi.hoisted(() => vi.fn());
const mockUseCommentReminder = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => `User-${id}`,
  }),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {},
}));

vi.mock("@multica/cerebro-channels", () => ({
  CommentCostBadge: () => null,
}));

vi.mock("@multica/cerebro-inbox", () => ({
  useCommentReminder: (issueId: string, options?: { forceEnabled?: boolean }) => {
    mockUseCommentReminder(issueId, options);
    return {
      enabled: true,
      label: "Remind me",
      openReminder: mockOpenReminder,
      dialog: <div data-testid="reminder-dialog" />,
    };
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <p>{content}</p>,
  ContentEditor: React.forwardRef(function MockContentEditor() {
    return <div data-testid="editor" />;
  }),
  copyMarkdown: vi.fn(),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => <div data-testid="drop-overlay" />,
}));

vi.mock("@multica/cerebro-attachments/views", () => ({
  AttachmentList: () => null,
}));

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: () => <button type="button">Attach</button>,
}));

vi.mock("@multica/ui/components/common/reaction-bar", () => ({
  ReactionBar: () => null,
}));

vi.mock("@multica/ui/components/common/quick-emoji-picker", () => ({
  QuickEmojiPicker: () => <button type="button">Emoji</button>,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props}>{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render, children }: { render?: React.ReactElement; children?: React.ReactNode }) => render ?? <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

const DropdownContext = React.createContext<{
  open: boolean;
  setOpen: (open: boolean) => void;
} | null>(null);

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({
    open,
    onOpenChange,
    children,
  }: {
    open?: boolean;
    onOpenChange?: (open: boolean) => void;
    children: React.ReactNode;
  }) => {
    const [internalOpen, setInternalOpen] = React.useState(false);
    const actualOpen = open ?? internalOpen;
    const setOpen = (next: boolean) => {
      onOpenChange?.(next);
      setInternalOpen(next);
    };
    return (
      <DropdownContext.Provider value={{ open: actualOpen, setOpen }}>
        <div data-testid="dropdown" data-open={actualOpen ? "true" : "false"}>
          {children}
        </div>
      </DropdownContext.Provider>
    );
  },
  DropdownMenuTrigger: ({ render }: { render: React.ReactElement }) => {
    const ctx = React.useContext(DropdownContext);
    const trigger = render as React.ReactElement<React.ButtonHTMLAttributes<HTMLButtonElement>>;
    return React.cloneElement(trigger, {
      onClick: (event: React.MouseEvent<HTMLButtonElement>) => {
        trigger.props.onClick?.(event);
        ctx?.setOpen(true);
      },
    });
  },
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => {
    const ctx = React.useContext(DropdownContext);
    return ctx?.open ? <div role="menu">{children}</div> : null;
  },
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" role="menuitem" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  AlertDialogAction: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
  AlertDialogCancel: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
  AlertDialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogDescription: ({ children }: { children: React.ReactNode }) => <p>{children}</p>,
  AlertDialogFooter: ({ children }: { children: React.ReactNode }) => <footer>{children}</footer>,
  AlertDialogHeader: ({ children }: { children: React.ReactNode }) => <header>{children}</header>,
  AlertDialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
}));

vi.mock("./cerebro-create-issue-from-message", () => ({
  CreateIssueFromMessageDialog: () => <div data-testid="create-issue-dialog" />,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (tree: {
      comment: Record<string, string>;
    }) => string) =>
      selector({
        comment: {
          copied_toast: "Copied",
          copy_action: "Copy",
          edit_action: "Edit",
          delete_action: "Delete",
          update_failed: "Update failed",
          edit_placeholder: "Edit",
          cancel_edit: "Cancel",
          save_action: "Save",
          delete_title: "Delete",
          delete_desc: "Delete this comment",
          delete_desc_with_replies: "Delete this thread",
          cancel_action: "Cancel",
        },
      }),
  }),
}));

import { SlackMessageView } from "./slack-message-view";

function entry(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    type: "comment",
    id: "m1",
    actor_type: "member",
    actor_id: "me",
    content: "Remember this later",
    created_at: "2026-06-10T10:00:00Z",
    parent_id: null,
    reactions: [],
    attachments: [],
    ...overrides,
  };
}

describe("SlackMessageView message actions", () => {
  beforeEach(() => {
    mockOpenReminder.mockClear();
    mockUseCommentReminder.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows Remind me in the message actions menu", async () => {
    const user = userEvent.setup();
    render(
      <SlackMessageView
        channelId="channel-1"
        topLevel={[entry()]}
        repliesByParent={new Map()}
        currentUserId="me"
        onOpenThread={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />,
    );

    await user.click(screen.getByLabelText("More actions"));
    await user.click(screen.getByRole("menuitem", { name: /remind me/i }));

    expect(mockOpenReminder).toHaveBeenCalledWith("m1", "Remember this later");
    expect(mockUseCommentReminder).toHaveBeenCalledWith("channel-1", {
      forceEnabled: true,
    });
  });

  it("keeps the mobile actions bar anchored inside the viewport", () => {
    render(
      <SlackMessageView
        channelId="channel-1"
        topLevel={[entry()]}
        repliesByParent={new Map()}
        currentUserId="me"
        onOpenThread={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />,
    );

    const moreActions = screen.getByLabelText("More actions");
    const actionsBar = moreActions.parentElement?.parentElement;

    expect(actionsBar).toHaveClass("left-2");
    expect(actionsBar).toHaveClass("sm:right-4");
  });

  it("opens the same actions menu on touch long-press", () => {
    vi.useFakeTimers();
    render(
      <SlackMessageView
        channelId="channel-1"
        topLevel={[entry()]}
        repliesByParent={new Map()}
        currentUserId="me"
        onOpenThread={vi.fn()}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />,
    );

    fireEvent.pointerDown(screen.getByText("Remember this later"), {
      pointerType: "touch",
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });

    expect(screen.getByTestId("dropdown")).toHaveAttribute("data-open", "true");
    expect(screen.getByRole("menuitem", { name: /remind me/i })).toBeInTheDocument();
  });
});
