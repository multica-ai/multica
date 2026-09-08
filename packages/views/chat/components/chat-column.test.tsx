import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";
import { ChatMessageSkeleton } from "./chat-message-list";
import { NoAgentBanner } from "./no-agent-banner";
import { ArchivedAgentBanner } from "./archived-agent-banner";
import { OfflineBanner } from "./offline-banner";
import { ChatQueue } from "./chat-queue";

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
  it.each([
    ["no-agent banner", <NoAgentBanner key="n" />],
    ["archived-agent banner", <ArchivedAgentBanner key="a" agentName="Lambda" />],
    ["offline banner", <OfflineBanner key="o" agentName="Lambda" availability="offline" />],
    ["unstable banner", <OfflineBanner key="u" agentName="Lambda" availability="unstable" />],
    ["message skeleton", <ChatMessageSkeleton key="s" />],
    [
      "follow-up queue",
      <ChatQueue
        key="q"
        headStatus="running"
        tasks={[
          {
            task_id: "task-queued",
            status: "queued",
            content: "Follow up",
            created_at: "2026-07-30T00:00:00Z",
          },
        ]}
        onSendNow={() => {}}
        onEdit={() => {}}
        onRemove={() => {}}
        onClear={() => {}}
      />,
    ],
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
