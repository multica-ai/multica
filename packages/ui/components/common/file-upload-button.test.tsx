import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FileUploadButton } from "./file-upload-button";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: () => "Attach file",
  }),
}));

describe("FileUploadButton", () => {
  afterEach(() => {
    cleanup();
  });

  it("passes every selected file to onSelectMany when multiple is enabled", () => {
    const onSelect = vi.fn();
    const onSelectMany = vi.fn();
    render(
      <FileUploadButton
        multiple
        onSelect={onSelect}
        onSelectMany={onSelectMany}
      />,
    );

    const input = document.querySelector("input[type='file']") as HTMLInputElement;
    const files = [
      new File(["first"], "first.txt", { type: "text/plain" }),
      new File(["second"], "second.txt", { type: "text/plain" }),
    ];
    fireEvent.change(input, { target: { files } });

    expect(input.multiple).toBe(true);
    expect(onSelectMany).toHaveBeenCalledWith(files);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("keeps single-file callers working when onSelectMany is omitted", () => {
    const onSelect = vi.fn();
    render(<FileUploadButton multiple onSelect={onSelect} />);

    const input = document.querySelector("input[type='file']") as HTMLInputElement;
    const first = new File(["first"], "first.txt", { type: "text/plain" });
    const second = new File(["second"], "second.txt", { type: "text/plain" });
    fireEvent.change(input, { target: { files: [first, second] } });

    expect(screen.getByRole("button", { name: "Attach file" })).toBeTruthy();
    expect(onSelect).toHaveBeenCalledTimes(2);
    expect(onSelect).toHaveBeenNthCalledWith(1, first);
    expect(onSelect).toHaveBeenNthCalledWith(2, second);
  });
});
