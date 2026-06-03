import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MobileAppOpenBanner } from "./mobile-app-open-banner";

describe("MobileAppOpenBanner", () => {
  it("renders a localized app link", () => {
    render(
      <MobileAppOpenBanner
        href="https://multica.wujieai.com/openharness/issues/OPE-2151?comment=c1"
        locale="zh-Hans"
      />,
    );

    expect(screen.getByText("已安装 Multica？")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "打开 App" })).toHaveAttribute(
      "href",
      "https://multica.wujieai.com/openharness/issues/OPE-2151?comment=c1",
    );
  });
});
