"use client";

// CEREBRO-PATCH(folder-suggestion): FIR-2697 part 2 — the editor banner for a
// pending folder suggestion. An agent proposed an existing folder for this
// document/note; the artifact has NOT moved. A person accepts (moves it) or
// dismisses (leaves it in place). Renders nothing when the feature is off, there
// is no pending proposal, or the caller cannot see the target folder (the query
// returns null in all three cases).
import { FolderInput, Check, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  useFolderSuggestionForArtifact,
  useAcceptFolderSuggestion,
  useRejectFolderSuggestion,
} from "@multica/cerebro-artifacts/core";

export function FolderSuggestionBanner({
  artifactId,
  canResolve,
}: {
  artifactId: string;
  // Whether this member may accept/reject (they can edit the artifact or are a
  // workspace admin). When false the banner is shown read-only so everyone sees
  // a proposal is pending, but only an authorized person can act on it.
  canResolve: boolean;
}) {
  const flagOn = useFeatureFlag("cerebro_folder_suggestions");
  const { data: suggestion } = useFolderSuggestionForArtifact(
    artifactId,
    flagOn,
  );
  const accept = useAcceptFolderSuggestion();
  const reject = useRejectFolderSuggestion();

  if (!flagOn || !suggestion) return null;

  const busy = accept.isPending || reject.isPending;
  const byAgent = suggestion.suggested_by_type === "agent";

  return (
    <div className="mt-3 flex flex-col gap-2 rounded-md border border-amber-300/60 bg-amber-50 p-3 text-sm dark:border-amber-500/30 dark:bg-amber-950/30">
      <div className="flex items-start gap-2">
        <FolderInput className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
        <div className="min-w-0">
          <span>
            {byAgent ? "An agent suggests" : "Suggested"} filing this in{" "}
            <span className="font-medium">
              {suggestion.folder_name || "a folder"}
            </span>
            .
          </span>
          {suggestion.reason ? (
            <span className="mt-0.5 block text-muted-foreground">
              “{suggestion.reason}”
            </span>
          ) : null}
        </div>
      </div>
      {canResolve ? (
        <div className="flex gap-2 pl-6">
          <Button
            size="sm"
            variant="default"
            disabled={busy}
            onClick={() =>
              accept.mutate({
                id: suggestion.id,
                artifact_id: suggestion.artifact_id,
              })
            }
          >
            <Check className="mr-1 size-3.5" />
            Accept & move
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={() =>
              reject.mutate({
                id: suggestion.id,
                artifact_id: suggestion.artifact_id,
              })
            }
          >
            <X className="mr-1 size-3.5" />
            Dismiss
          </Button>
        </div>
      ) : (
        <div className="pl-6 text-xs text-muted-foreground">
          Waiting for a teammate to accept before it moves.
        </div>
      )}
    </div>
  );
}
