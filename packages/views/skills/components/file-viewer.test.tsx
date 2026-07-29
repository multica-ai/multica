// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type {
  ButtonHTMLAttributes,
  ReactNode,
  TextareaHTMLAttributes,
} from "react";

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/textarea", () => ({
  Textarea: (props: TextareaHTMLAttributes<HTMLTextAreaElement>) => (
    <textarea {...props} />
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render: trigger }: { render: ReactNode }) => <>{trigger}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/cerebro-skill-ownership/views", () => ({
  CerebroSkillMarkdownEditor: ({ content }: { content: string }) => (
    <div data-testid="rich-skill-editor">{content}</div>
  ),
}));

vi.mock("../../common/markdown", () => ({
  Markdown: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("../../editor", () => ({
  ContentEditor: () => <div />,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "" }),
}));

import { FileViewer } from "./file-viewer";

describe("FileViewer", () => {
  it("opens Markdown files in the rich-text editor", () => {
    render(
      <FileViewer
        path="SKILL.md"
        content="# Human-readable skill"
        onChange={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button"));

    expect(screen.getByTestId("rich-skill-editor")).toHaveTextContent(
      "Human-readable skill",
    );
  });

  it("keeps MDX files on the raw editor to avoid changing MDX syntax", () => {
    render(
      <FileViewer
        path="reference.mdx"
        content="<CustomComponent />"
        onChange={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button"));

    expect(screen.queryByTestId("rich-skill-editor")).toBeNull();
    expect(screen.getByRole("textbox")).toHaveValue("<CustomComponent />");
  });
});
