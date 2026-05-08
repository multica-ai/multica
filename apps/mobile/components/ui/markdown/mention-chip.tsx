import { Text } from "react-native";

import { COLOR } from "./theme";

// multica's mention protocol: `[label](mention://type/id)` where type is one
// of member / agent / issue / all. Backend regex parses this from comment
// markdown to trigger agent tasks (when type='agent'), so wire format must
// be preserved verbatim — only display style is mobile-specific.
//
// PARITY (see apps/mobile/CLAUDE.md): visual matches web's `.mention` rule
// at packages/views/editor/content-editor.css:451 — primary color, semibold,
// no background fill. Web does NOT differentiate by type for member/agent/
// all; Agent identity is communicated via the comment author's avatar +
// "Agent" label, not via mention color.
//
// Issue mentions on web get a richer `<IssueChip>` (status icon + identifier
// + title). Mobile renders them as plain mention text for now — porting the
// chip needs cache lookups + status rendering, tracked separately.
export function MentionChip({
  label,
}: {
  type: "member" | "agent" | "issue" | "all" | string;
  label: string;
}) {
  return (
    <Text
      style={{
        color: COLOR.primary,
        fontWeight: "600",
      }}
    >
      {label}
    </Text>
  );
}
