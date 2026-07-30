import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";
import { ChatMessageSkeleton } from "./chat-message-list";
import { NoAgentBanner } from "./no-agent-banner";
import { ArchivedAgentBanner } from "./archived-agent-banner";
import { OfflineBanner } from "./offline-banner";

// Every layer of the chat body has to land on the same left/right edges: the
// message column, the status banner above the composer, and the composer card.
// They drifted once already — the message list capped `max-w-4xl` with its
// padding INSIDE the cap while the composer put the padding outside, so on any
// surface wider than ~936px the text sat 20px narrower than the box below it.
// These tests pin the shared two-layer contract that fixed it.

const TEST_RESOURCES = { en: { chat: enChat } };

function renderChat(ui: React.ReactElement) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {ui}
    </I18nProvider>,
  );
}

const GUTTER_CLASSES = CHAT_GUTTER.split(" ");
const COLUMN_CLASSES = CHAT_COLUMN.split(" ");

/** Outermost element of a rendered chat-body layer. */
function root(container: HTMLElement): HTMLElement {
  const el = container.firstElementChild;
  if (!(el instanceof HTMLElement)) throw new Error("layer rendered nothing");
  return el;
}

describe("chat column geometry", () => {
  it("keeps the gutter a container query, not a viewport one", () => {
    // The chat body renders in a resizable split pane, a 360px floating window,
    // and the agent builder — all independent widths inside one browser window,
    // so a `sm:`/`lg:` variant would widen the floating window's gutter just
    // because the page behind it is wide.
    for (const cls of GUTTER_CLASSES) {
      if (cls.includes(":")) expect(cls).toMatch(/^@/);
    }
    // Base gutter with no variant, so a host that forgets `@container` degrades
    // to the old flat spacing instead of losing its padding entirely.
    expect(GUTTER_CLASSES).toContain("px-5");
  });

  it("puts the gutter OUTSIDE the width cap, never on one element", () => {
    // This is the invariant that broke: a single element carrying both means
    // the padding eats into the cap, and that layer ends up narrower than its
    // siblings once the surface is wider than the cap.
    for (const cls of GUTTER_CLASSES) {
      expect(COLUMN_CLASSES).not.toContain(cls);
    }
    expect(COLUMN_CLASSES).toContain("max-w-4xl");
    expect(GUTTER_CLASSES.some((c) => c.includes("max-w"))).toBe(false);
  });

  it.each([
    ["no-agent banner", <NoAgentBanner key="n" />],
    ["archived-agent banner", <ArchivedAgentBanner key="a" agentName="Lambda" />],
    ["offline banner", <OfflineBanner key="o" agentName="Lambda" availability="offline" />],
    ["unstable banner", <OfflineBanner key="u" agentName="Lambda" availability="unstable" />],
    ["message skeleton", <ChatMessageSkeleton key="s" />],
  ])("aligns the %s on the shared gutter + column", (_label, ui) => {
    const { container } = renderChat(ui);
    const outer = root(container);
    const inner = outer.firstElementChild as HTMLElement;

    for (const cls of GUTTER_CLASSES) expect(outer).toHaveClass(cls);
    for (const cls of COLUMN_CLASSES) expect(inner).toHaveClass(cls);
    // The cap belongs to the inner box only — see the test above.
    expect(outer.className).not.toContain("max-w-");
  });

  it("does not double the gutter when the skeleton nests inside the list", () => {
    // ChatMessageList's pre-mount frame is already inside the gutter, so it
    // renders the skeleton BODY; only the standalone export carries a gutter.
    const { container } = renderChat(<ChatMessageSkeleton />);
    const gutters = container.querySelectorAll(`.${CSS.escape(GUTTER_CLASSES[0]!)}`);
    expect(gutters).toHaveLength(1);
  });
});
