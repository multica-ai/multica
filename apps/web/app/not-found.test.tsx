import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "@multica/views/locales";
import NotFound from "./not-found";

describe("NotFound", () => {
  it("renders the not-found page in the selected locale", () => {
    render(
      <I18nProvider locale="zh-Hans" resources={RESOURCES}>
        <NotFound />
      </I18nProvider>,
    );

    expect(
      screen.getByRole("heading", { name: "页面未找到" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("你要查找的页面不存在或已被移动。"),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "返回 Multica" })).toHaveAttribute(
      "href",
      "/",
    );
  });
});
