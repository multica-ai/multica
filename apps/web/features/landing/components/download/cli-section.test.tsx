import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { CliSection } from "./cli-section";

vi.mock("../../i18n", () => ({
  useLocale: () => ({
    t: {
      download: {
        cli: {
          title: "Install the connector",
          sub: "Connect a computer.",
          platformLabel: "Operating system",
          platformUnix: "macOS / Linux",
          platformWindows: "Windows",
          installLabel: "Install",
          startLabel: "Connect",
          copyLabel: "Copy",
          copiedLabel: "Copied",
          sshNote: "Works over SSH.",
        },
      },
    },
  }),
}));

describe("CliSection", () => {
  it("switches the connector installer between macOS/Linux and Windows", async () => {
    const user = userEvent.setup();
    const { baseElement } = render(<CliSection />);

    expect(screen.getByRole("button", { name: "macOS / Linux" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(baseElement).toHaveTextContent(
      "curl -fsSL https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.sh | bash",
    );

    await user.click(screen.getByRole("button", { name: "Windows" }));

    expect(screen.getByRole("button", { name: "Windows" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(baseElement).toHaveTextContent(
      "irm https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.ps1 | iex",
    );
    expect(baseElement).not.toHaveTextContent("curl -fsSL");
  });
});
