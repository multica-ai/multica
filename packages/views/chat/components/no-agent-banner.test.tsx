import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";
import { NoAgentBanner } from "./no-agent-banner";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

describe("NoAgentBanner", () => {
  it("renders in the floating chat window", () => {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <NoAgentBanner />
      </I18nProvider>,
    );

    expect(screen.getByText("You need an agent to start chatting.")).not.toBeNull();
  });

  it("does not render in the full-page obitaPlus layout", () => {
    const { container } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <NoAgentBanner layout="page" />
      </I18nProvider>,
    );

    expect(container.firstChild).toBeNull();
  });
});
