// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { SkillFilesEditor } from "./skill-files-editor";

vi.mock("./file-viewer", () => ({
  isMarkdownPath: () => false,
  FileViewer: () => <div data-testid="file-viewer" />,
}));

function renderEditor(
  overrides: Partial<{
    onAddFile: (path: string) => void;
    onCancelAddFile: () => void;
  }> = {},
) {
  const onAddFile = overrides.onAddFile ?? vi.fn();
  const onCancelAddFile = overrides.onCancelAddFile ?? vi.fn();
  renderWithI18n(
    <SkillFilesEditor
      filePaths={["SKILL.md"]}
      selectedPath="SKILL.md"
      selectedContent=""
      mode="raw"
      canEdit
      addingFile
      focusEditor={false}
      onSelectPath={vi.fn()}
      onModeChange={vi.fn()}
      onStartAddFile={vi.fn()}
      onAddFile={onAddFile}
      onCancelAddFile={onCancelAddFile}
      onDeleteFile={vi.fn()}
      onRenameFile={vi.fn()}
      onEditFile={vi.fn()}
      onContentChange={vi.fn()}
      onFocusHandled={vi.fn()}
    />,
  );
  const input = screen.getByPlaceholderText("templates/review.md") as HTMLInputElement;
  return { input, onAddFile, onCancelAddFile };
}

describe("SkillFilesEditor add-file IME composition", () => {
  it.each([
    ["standard composition signal", { isComposing: true, keyCode: 13 }],
    ["Safari composition signal", { isComposing: false, keyCode: 229 }],
  ])("does not add a file with a composing Enter (%s)", (_name, eventInit) => {
    const { input, onAddFile } = renderEditor();
    fireEvent.change(input, { target: { value: "xiangmu.md" } });
    fireEvent.keyDown(input, { key: "Enter", ...eventInit });
    expect(onAddFile).not.toHaveBeenCalled();
    expect(input).toHaveValue("xiangmu.md");
  });

  it.each([
    ["standard composition signal", { isComposing: true, keyCode: 27 }],
    ["Safari composition signal", { isComposing: false, keyCode: 229 }],
  ])("does not cancel with a composing Escape (%s)", (_name, eventInit) => {
    const { input, onAddFile, onCancelAddFile } = renderEditor();
    fireEvent.change(input, { target: { value: "xiangmu.md" } });
    fireEvent.keyDown(input, { key: "Escape", ...eventInit });
    expect(onAddFile).not.toHaveBeenCalled();
    expect(onCancelAddFile).not.toHaveBeenCalled();
    expect(input).toHaveValue("xiangmu.md");
  });

  it("adds the path once after composition ends", () => {
    const { input, onAddFile } = renderEditor();
    fireEvent.change(input, { target: { value: "项目.md" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: false, keyCode: 13 });
    expect(onAddFile).toHaveBeenCalledTimes(1);
    expect(onAddFile).toHaveBeenCalledWith("项目.md");
  });
});
