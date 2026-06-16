"use client";

// NoteReferences (TECH-3421) — the "References" section shown above a note's
// body. It mirrors the issue-reference list (@multica/cerebro-references) but
// for notes: a compact list of the things this note points at, each clickable
// to navigate, plus an "Add reference" picker.
//
// For the MVP the only object type is `issue`. The rendering and the add picker
// both switch on `ref.object`, so adding business objects later is a localized
// change (a new case in objectHref/objectLabel + a new picker source) rather
// than a rewrite.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Link2, ListTodo, Plus, X } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
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
    <div className="group flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5 text-sm">
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
      <Button
        variant="ghost"
        size="icon"
        className="size-6 shrink-0 opacity-0 group-hover:opacity-100"
        disabled={removing}
        onClick={() => onRemove(reference)}
        aria-label="Remove reference"
      >
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

function AddReferencePicker({ noteId }: { noteId: string }) {
  const addReference = useAddNoteReference(noteId);
  const paths = useWorkspacePaths();
  const [open, setOpen] = React.useState(false);
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
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button variant="ghost" size="sm" className="h-7 gap-1 text-xs">
            <Plus className="size-3.5" />
            Add reference
          </Button>
        }
      />
      <PopoverContent align="end" className="w-80 p-0">
        <Command shouldFilter={false}>
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
      </PopoverContent>
    </Popover>
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

  // Hide the whole section while the first load is in flight and there is
  // nothing cached yet — avoids a flash of the empty state.
  if (isLoading && references.length === 0) return null;

  return (
    <div data-testid="note-reference-list" className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <Link2 className="size-3.5" aria-hidden="true" />
          References
        </div>
        <AddReferencePicker noteId={noteId} />
      </div>

      {references.length > 0 && (
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
      )}
    </div>
  );
}
