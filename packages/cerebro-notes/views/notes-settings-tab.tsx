"use client";

import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  CerebroFlagRow,
  useFeatureFlagsQuery,
  type CerebroFlagKey,
} from "@multica/cerebro-feature-flags";

// The note features the user can switch on/off (TECH-3637). These flags exist
// in the registry but had no settings surface, which is why Jesper couldn't
// find where to turn Notes (and its parts) on or off. Each row is the same
// personal/whole-workspace 3-way control used in the Cerebro features tab.
const NOTE_FLAGS: { key: CerebroFlagKey; label: string; description: string }[] =
  [
    {
      key: "cerebro_notes",
      label: "Notes",
      description:
        "The Notes surface — the nav entry, quick capture, the notes list + editor, and the Notes box in the dynamic inbox. Turn this off to hide Notes entirely.",
    },
    {
      key: "cerebro_note_comments",
      label: "Comments & suggestions",
      description:
        "Select text in a note to attach a comment or propose an edit; the note owner can accept or reject a suggestion.",
    },
    {
      key: "cerebro_note_versions",
      label: "Version history",
      description:
        "Keep a history of changes to a note and restore an earlier version.",
    },
    {
      key: "cerebro_note_lock",
      label: "Edit lock",
      description:
        "Hold a single-writer lock while editing so two people don't overwrite each other.",
    },
    {
      key: "cerebro_note_types",
      label: "Recurring notes",
      description:
        "Reusable note templates that recur on a schedule (e.g. a weekly business review).",
    },
  ];

export function NotesSettingsTab() {
  const query = useFeatureFlagsQuery();
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Notes</h2>
        <p className="text-xs text-muted-foreground">
          Turn the Notes feature and its parts on or off. Workspace owners can
          force a setting on or off for everyone.
        </p>
      </div>

      {query.isError && (
        <p className="text-xs text-destructive">
          Failed to load settings:{" "}
          {query.error instanceof Error ? query.error.message : "unknown error"}
        </p>
      )}

      <div className="space-y-3">
        {NOTE_FLAGS.map((flag) => (
          <CerebroFlagRow
            key={flag.key}
            flagKey={flag.key}
            label={flag.label}
            description={flag.description}
            isAdmin={isAdmin}
          />
        ))}
      </div>
    </section>
  );
}
