"use client";

// NoteReferences (TECH-3421) — the "References" section shown above a note's
// body. It mirrors the issue-reference list (@multica/cerebro-references) but
// for notes: a compact list of the things this note points at, each clickable
// to navigate, plus a remove control per row.
//
// Adding a reference is driven from the note's "⋯" actions menu via the
// controlled NoteAddReferenceDialog (TECH-3690), so the references header stays
// clutter-free and the action is discoverable on mobile too.
//
// For the MVP the only object type is `issue`. The rendering and the add picker
// both switch on `ref.object`, so adding business objects later is a localized
// change (a new case in objectHref/objectLabel + a new picker source) rather
// than a rewrite.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Link2, ListTodo, X } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "@multica/views/navigation";
import {
  useAddNoteReference,
  useDeleteNoteReference,
  useNoteReferences,
  type NoteReference,
} from "../core";

function truncate(s: string, max = 60): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

// Small debounce so typing in the picker does not fire a search per keystroke.
function useDebounced(value: string, ms = 200): string {
  const [debounced, setDebounced] = React.useState(value);
  React.useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

// objectHref derives the in-app destination for a reference. Switch on
// ref.object so new object kinds plug in here without touching the list. An
// unknown object (or one without a usable id) yields null and renders as
// non-clickable, never a crash.
function objectHref(
  ref: NoteReference,
  paths: ReturnType<typeof useWorkspacePaths>,
): string | null {
  switch (ref.object) {
    case "issue":
      return ref.ref_id ? paths.issueDetail(ref.ref_id) : null;
    default:
      return ref.url || null;
  }
}

function objectLabel(ref: NoteReference): string {
  return ref.label?.trim() || ref.ref_id || ref.object || "Reference";
}

function ReferenceRow({
  reference,
  onOpen,
  onRemove,
  removing,
}: {
  reference: NoteReference;
  onOpen: (ref: NoteReference) => void;
  onRemove: (ref: NoteReference) => void;
  removing: boolean;
}) {
  const clickable = objectHref(reference, useWorkspacePaths()) !== null;
  return (
    <div className="flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5 text-sm">
      <ListTodo
        className="size-4 shrink-0 text-muted-foreground"
        aria-hidden="true"
      />
      <button
        type="button"
        disabled={!clickable}
        onClick={() => onOpen(reference)}
        className={cn(
          "min-w-0 flex-1 truncate text-left",
          clickable ? "hover:underline" : "cursor-default",
        )}
        title={objectLabel(reference)}
      >
        {objectLabel(reference)}
      </button>
      {/* Remove is always visible — a hover-only affordance was invisible and
          unusable on touch devices (TECH-3690). */}
      <Button
        variant="ghost"
        size="icon-xs"
        className="shrink-0 text-muted-foreground hover:text-foreground"
        disabled={removing}
        onClick={() => onRemove(reference)}
        aria-label="Remove reference"
      >
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

// NoteAddReferenceDialog is the controlled issue picker. It is opened from the
// note's "⋯" actions menu (TECH-3690) rather than an inline button, so it lives
// as a self-contained dialog with no anchor of its own.
export function NoteAddReferenceDialog({
  noteId,
  open,
  onOpenChange,
}: {
  noteId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const addReference = useAddNoteReference(noteId);
  const paths = useWorkspacePaths();
  const [q, setQ] = React.useState("");
  const query = useDebounced(q.trim());

  React.useEffect(() => {
    if (open) setQ("");
  }, [open]);

  const { data: issueRes } = useQuery({
    queryKey: ["note-reference-picker", "issues", query],
    queryFn: () =>
      api.searchIssues({ q: query, limit: 8, include_closed: true }),
    enabled: open && query.length > 0,
  });

  // The search endpoint already restricts to real issues server-side
  // (`AND i.kind = 'issue'`). Don't re-filter on `kind` here: the search
  // response leaves `kind` empty, so a `kind === "issue"` filter dropped every
  // result and the picker found nothing (TECH-3637).
  const issues = issueRes?.issues ?? [];

  const pickIssue = (issue: {
    id: string;
    identifier: string;
    title: string;
  }) => {
    addReference.mutate({
      object: "issue",
      ref_id: issue.id,
      label: truncate(`${issue.identifier} ${issue.title}`),
      url: paths.issueDetail(issue.id),
    });
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md gap-3 p-0">
        <DialogHeader className="px-4 pt-4">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Link2 className="size-4" />
            Add reference
          </DialogTitle>
        </DialogHeader>
        <Command shouldFilter={false} className="rounded-t-none">
          <CommandInput
            value={q}
            onValueChange={setQ}
            placeholder="Search an issue to link…"
            autoFocus
          />
          <CommandList className="max-h-72">
            {query.length === 0 && (
              <CommandEmpty>Type to search issues…</CommandEmpty>
            )}
            {query.length > 0 && issues.length === 0 && (
              <CommandEmpty>No matching issues.</CommandEmpty>
            )}
            {issues.length > 0 && (
              <CommandGroup heading="Issues">
                {issues.map((i) => (
                  <CommandItem
                    key={`issue:${i.id}`}
                    value={`issue:${i.id}`}
                    onSelect={() => pickIssue(i)}
                  >
                    <ListTodo className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">
                      <span className="text-muted-foreground">
                        {i.identifier}
                      </span>{" "}
                      {i.title}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  );
}

export function NoteReferences({ noteId }: { noteId: string }) {
  const { data: references = [], isLoading } = useNoteReferences(noteId);
  const deleteReference = useDeleteNoteReference(noteId);
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const [pendingDeleteId, setPendingDeleteId] = React.useState<string | null>(
    null,
  );

  const openReference = (ref: NoteReference) => {
    const href = objectHref(ref, paths);
    if (href) navigation.push(href);
  };

  const removeReference = (ref: NoteReference) => {
    setPendingDeleteId(ref.id);
    deleteReference.mutate(ref.id, {
      onSettled: () => setPendingDeleteId(null),
    });
  };

  // Nothing to show until there is at least one reference. Adding is driven
  // from the "⋯" menu (TECH-3690), so an empty header would just be noise.
  if (isLoading && references.length === 0) return null;
  if (references.length === 0) return null;

  return (
    <div data-testid="note-reference-list" className="flex flex-col gap-2">
      <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Link2 className="size-3.5" aria-hidden="true" />
        References
      </div>

      <div className="flex flex-col gap-1.5">
        {references.map((reference) => (
          <ReferenceRow
            key={reference.id}
            reference={reference}
            onOpen={openReference}
            onRemove={removeReference}
            removing={pendingDeleteId === reference.id}
          />
        ))}
      </div>
    </div>
  );
}
