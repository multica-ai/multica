import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import en from "../locales/en";
import zhHans from "../locales/zh-Hans";
import { HeroFrame } from "./heroes";
import { WHATS_NEW_RELEASES } from "./releases";

const withHeroes = WHATS_NEW_RELEASES.flatMap((r) => r.highlights).filter((h) => h.hero);

function renderHero(id: string, locale: "en" | "zh-Hans") {
  const hero = withHeroes.find((h) => h.id === id)!.hero!;
  return render(
    <I18nProvider locale={locale} resources={{ [locale]: locale === "en" ? en : zhHans }}>
      <HeroFrame hero={hero} />
    </I18nProvider>,
  );
}

describe("What's new hero art", () => {
  it.each([
    ["community-themes", ["Themes", "Search themes", "Install", "In use"]],
    ["environment-controls", ["Environments", "2 running", "Running", "Switch to window", "View logs"]],
    ["chat-annotations", ["1 annotation", "Add a comment...", "Add"]],
    ["inbox-snooze", ["Inbox", "Snooze until...", "1 hour", "3 hours", "1 day", "3 days"]],
    ["desktop-voice-input", ["Release to insert", "Leave a comment..."]],
    ["chat-wakeups", ["Will report back: PR #128", "ends in 24 hours", "Back to report · it happened · PR #128"]],
    ["chat-steering", ["Making edits", "Add to current run"]],
    ["weixin-channel", ["Connect messaging channel", "Scan with WeChat and confirm on your phone."]],
    ["project-folders", ["Projects", "Atlas", "Orbit", "API"]],
    ["inbox-tabs", ["Inbox", "3", "Snoozed", "Archived"]],
    ["project-overview", ["Overview", "Issues", "Activity", "Progress", "8 of 12 issues done"]],
  ])("draws %s from the product's own strings", (id, texts) => {
    const { container } = renderHero(id, "en");
    for (const text of texts) expect(container).toHaveTextContent(text);
  });

  it("follows the locale", () => {
    const { container } = renderHero("environment-controls", "zh-Hans");
    expect(container).not.toHaveTextContent("Environments");
    expect(container).not.toHaveTextContent("environments.");
  });

  it("stays out of the accessibility tree", () => {
    const { container } = renderHero("community-themes", "en");
    expect(container.firstElementChild).toHaveAttribute("aria-hidden", "true");
  });
});
