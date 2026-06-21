"use client";

// NoteSuggestIssueReference (FIR-1753) — closes the loop Jesper asked for: when
// a note comment @-tags an issue but the note is not yet linked to that issue,
// offer to add it as a reference in one click. Linking is what makes the note a
// real send destination (the agent-collab "Send all" flow resolves the coupling
// from the note's references), so this turns "I mentioned the issue" into "the
// note is coupled to it" without sending the user off to the References panel.
//
// It only suggests issues the note is NOT already linked to, and disappears the
// moment a link is added.
import * as React from "react";
import { Link2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAddNoteReference, type IssueMention } from "../core";

export function NoteSuggestIssueReference({
  noteId,
  issues,
}: {
  noteId: string;
  // Distinct issues @-tagged in the note's comments that are not yet linked.
  issues: IssueMention[];
}) {
  const addReference = useAddNoteReference(noteId);
  const paths = useWorkspacePaths();
  const [adding, setAdding] = React.useState<string | null>(null);

  if (issues.length === 0) return null;

  async function link(issue: IssueMention) {
    setAdding(issue.id);
    try {
      await addReference.mutateAsync({
        object: "issue",
        ref_id: issue.id,
        label: issue.label,
        url: paths.issueDetail(issue.id),
      });
      toast.success(`Linked ${issue.label} to this note`);
    } catch {
      toast.error("Couldn't link the issue. Try again.");
    } finally {
      setAdding(null);
    }
  }

  return (
    <div className="flex flex-col gap-2 border-b border-sky-400/40 bg-sky-400/10 px-3 py-2">
      <div className="flex items-center gap-1.5 text-xs">
        <Link2 className="size-3.5 text-sky-600" />
        <span className="font-medium">
          {issues.length === 1
            ? "A comment mentions a task that isn’t linked to this note"
            : "Comments mention tasks that aren’t linked to this note"}
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {issues.map((issue) => (
          <Button
            key={issue.id}
            size="sm"
            variant="secondary"
            onClick={() => link(issue)}
            disabled={adding !== null}
          >
            <Link2 className="size-3.5" />
            {adding === issue.id ? "Linking…" : `Link ${issue.label}`}
          </Button>
        ))}
      </div>
    </div>
  );
}
