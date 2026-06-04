import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MobileAppOpenBanner } from "./mobile-app-open-banner";

describe("MobileAppOpenBanner", () => {
  it("renders a localized app link outside WeChat", () => {
    render(
      <MobileAppOpenBanner
        href="wujieai-multicam://openharness/issues/OPE-2151?comment=c1"
        locale="zh-Hans"
      />,
    );

    expect(screen.getByText("已安装 Multica？")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "打开 App" })).toHaveAttribute(
      "href",
      "wujieai-multicam://openharness/issues/OPE-2151?comment=c1",
    );
  });

  it("renders an open-in-browser prompt for WeChat", () => {
    render(<MobileAppOpenBanner locale="zh-Hans" mode="wechat" />);

    expect(screen.getByText("请在浏览器中打开")).toBeInTheDocument();
    expect(screen.getByText("点击右上角菜单，选择在浏览器中打开后再打开 App。")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "打开 App" })).not.toBeInTheDocument();
  });
});
