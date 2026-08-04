// FIR-4350 — the placement matrix: for each conversation type, whether it is
// shown on the Chat page, in the Inbox, or both. Renders under the
// cerebro_chat_page toggle in the Cerebro features tab, the same way four other
// features attach their settings (see FLAG_SETTINGS in settings-tab.tsx).
"use client";

import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import {
  canTurnOff,
  type ConversationKind,
  type PlacementEntry,
} from "./chat-placement";
import { useChatPlacement } from "./use-chat-placement";

// FIR-4350 — agent chats are inbox-only (not configurable), so only Channels
// and DM appear in the matrix. See AGENT_PLACEMENT in chat-placement.ts.
const CONFIGURABLE_KINDS: readonly ConversationKind[] = ["channel", "dm"];

const KIND_LABEL: Record<ConversationKind, string> = {
  channel: "Channels",
  dm: "DM",
  agent_chat: "Agent chat",
};

const PLACES: Array<{ key: keyof PlacementEntry; label: string }> = [
  { key: "chat", label: "Chat" },
  { key: "inbox", label: "Inbox" },
];

export function ChatPlacementSettings() {
  const { placement, setPlacement } = useChatPlacement();

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        Where each kind of conversation is shown. Turning off Inbox hides those rows
        from the Inbox — nothing is deleted, archived or marked read, and they come
        back unchanged when you turn it on again.
      </p>
      <div className="grid grid-cols-[1fr_auto_auto] items-center gap-x-6 gap-y-2">
        <span />
        {PLACES.map((place) => (
          <span
            key={place.key}
            className="text-center text-xs font-medium text-muted-foreground"
          >
            {place.label}
          </span>
        ))}
        {CONFIGURABLE_KINDS.map((kind) => (
          <PlacementRow
            key={kind}
            kind={kind}
            entry={placement[kind]}
            canTurnOffPlace={(place) => canTurnOff(placement, kind, place)}
            onToggle={(place, next) => setPlacement(kind, place, next)}
          />
        ))}
      </div>
      <p className="text-xs text-muted-foreground">Agent chats always stay in the Inbox.</p>
    </div>
  );
}

function PlacementRow({
  kind,
  entry,
  canTurnOffPlace,
  onToggle,
}: {
  kind: ConversationKind;
  entry: PlacementEntry;
  canTurnOffPlace: (place: keyof PlacementEntry) => boolean;
  onToggle: (place: keyof PlacementEntry, next: boolean) => void;
}) {
  return (
    <>
      {/* Labels the whole row, not one switch — no htmlFor, or clicking the
          name would toggle whichever switch it pointed at. */}
      <Label className="text-sm font-normal">{KIND_LABEL[kind]}</Label>
      {PLACES.map((place) => {
        // A type must always be somewhere, so its last remaining place is
        // locked rather than allowed to disappear.
        const locked = entry[place.key] && !canTurnOffPlace(place.key);
        return (
          <span
            key={place.key}
            className="flex justify-center"
            // The tooltip lives on the wrapper, not the Switch: a disabled
            // element gets no pointer events, so a title on it never shows.
            title={
              locked
                ? `${KIND_LABEL[kind]} has to stay somewhere — turn on the other one first.`
                : undefined
            }
          >
            <Switch
              id={`placement-${kind}-${place.key}`}
              checked={entry[place.key]}
              disabled={locked}
              onCheckedChange={(next: boolean) => onToggle(place.key, next)}
              aria-label={`${KIND_LABEL[kind]} in ${place.label}`}
            />
          </span>
        );
      })}
    </>
  );
}
