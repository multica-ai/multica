"use client";

import { useEffect, useMemo, useState } from "react";
import { Link2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCreateIssueLink } from "@multica/core/links/mutations";
import { api } from "@multica/core/api";
import {
  LINK_TYPES,
  LINK_LABEL,
  MAX_LINKS_PER_ISSUE,
  type IssueLink,
  type LinkType,
} from "@multica/core/types";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
  PickerSection,
} from "./property-picker";

/**
 * Link picker — two-step UI:
 *   1. choose a link type (blocks / depends_on / duplicates / relates_to)
 *   2. search for the target issue (current workspace; debounced)
 *
 * Uses /api/issues/search which is workspace-scoped. Cross-workspace links
 * are supported by the backend but require a workspace switch first; we
 * keep the picker simple and route cross-workspace via the search results
 * if the user has multiple workspaces in the same context (out of scope
 * for L-PR#3 — left as a follow-up if needed).
 *
 * Already-attached targets are dimmed so the user doesn't double-link.
 * The same target can carry multiple link types in parallel — only the
 * (source, target, link_type) tuple is unique on the server.
 */
export function LinkPicker({
  issueId,
  attached,
  trigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align = "start",
}: {
  issueId: string;
  attached: IssueLink[];
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;

  const [linkType, setLinkType] = useState<LinkType | null>(null);
  const [filter, setFilter] = useState("");
  const [debounced, setDebounced] = useState("");

  const wsId = useWorkspaceId();
  const create = useCreateIssueLink();

  // Debounce the search input so we don't hammer /search on every keystroke.
  useEffect(() => {
    const t = setTimeout(() => setDebounced(filter.trim()), 200);
    return () => clearTimeout(t);
  }, [filter]);

  const search = useQuery({
    queryKey: ["issues", wsId, "search", debounced],
    queryFn: ({ signal }) =>
      api.searchIssues({ q: debounced, limit: 20, signal }),
    enabled: open && !!linkType && debounced.length >= 1,
    staleTime: 5_000,
  });

  // Compute which (target × link_type) tuples are already attached so we
  // can disable them in the picker.
  const attachedTuples = useMemo(() => {
    const s = new Set<string>();
    for (const l of attached) {
      // For a "blocks outgoing" we record the canonical (target, blocks)
      // tuple. The mirror "incoming" is just a view of the same physical
      // link, so we only check outgoing rows here — the user is creating
      // *new* outgoing links from this issue.
      if (l.direction === "outgoing") {
        s.add(`${l.target_issue_id}|${l.link_type}`);
      }
    }
    return s;
  }, [attached]);

  const atLimit = attached.length >= MAX_LINKS_PER_ISSUE;

  const handlePick = (
    targetIssueId: string,
    targetIdentifier: string,
    targetTitle: string,
  ) => {
    if (!linkType) return;
    if (atLimit) return;
    create.mutate({
      issueId,
      targetIssueId,
      linkType,
      optimisticTarget: {
        identifier: targetIdentifier,
        title: targetTitle,
        status: "todo",
        number: 0,
        workspace_id: wsId,
        workspace_name: "",
        workspace_slug: "",
      },
    });
    // Reset for another link, keep the picker open so the user can chain.
    setFilter("");
    setDebounced("");
  };

  const reset = () => {
    setLinkType(null);
    setFilter("");
    setDebounced("");
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) reset();
      }}
      width="w-72"
      align={align}
      searchable={!!linkType}
      searchPlaceholder={
        linkType
          ? `Search issues to ${LINK_LABEL[linkType].outgoing}...`
          : ""
      }
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        trigger ?? (
          <>
            <Link2 className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">
              {attached.length === 0 ? "Add link" : `${attached.length} link${attached.length === 1 ? "" : "s"}`}
            </span>
          </>
        )
      }
    >
      {/* Step 1: pick the link type */}
      {!linkType && (
        <PickerSection label="Link type">
          {LINK_TYPES.map((t) => (
            <PickerItem
              key={t}
              selected={false}
              onClick={() => setLinkType(t)}
            >
              <span className="capitalize">{LINK_LABEL[t].outgoing}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {/* Step 2: pick the target issue */}
      {linkType && (
        <>
          <div className="flex items-center justify-between border-b px-2 pb-1.5 pt-1 text-[11px] text-muted-foreground">
            <span>
              This issue <span className="font-medium">{LINK_LABEL[linkType].outgoing}</span>...
            </span>
            <button
              className="text-foreground underline-offset-2 hover:underline"
              onClick={reset}
            >
              change
            </button>
          </div>

          {atLimit && (
            <div className="px-2 py-2 text-[11px] text-muted-foreground">
              Max {MAX_LINKS_PER_ISSUE} links per issue.
            </div>
          )}

          {!atLimit && debounced.length === 0 && (
            <div className="px-2 py-3 text-center text-xs text-muted-foreground">
              Type to search issues by title or identifier.
            </div>
          )}

          {!atLimit && debounced.length > 0 && search.isLoading && (
            <div className="px-2 py-3 text-center text-xs text-muted-foreground">
              Searching...
            </div>
          )}

          {!atLimit &&
            debounced.length > 0 &&
            search.data &&
            search.data.issues.length === 0 && <PickerEmpty />}

          {!atLimit &&
            search.data?.issues.map((iss) => {
              const tupleKey = `${iss.id}|${linkType}`;
              const alreadyLinked = attachedTuples.has(tupleKey);
              const isSelf = iss.id === issueId;
              const disabled = alreadyLinked || isSelf;
              return (
                <PickerItem
                  key={iss.id}
                  selected={alreadyLinked}
                  disabled={disabled}
                  onClick={() => {
                    if (disabled) return;
                    handlePick(iss.id, iss.identifier, iss.title);
                  }}
                >
                  <div className="flex min-w-0 flex-col">
                    <div className="flex items-center gap-1.5 text-xs">
                      <span className="font-mono text-[11px] text-muted-foreground">
                        {iss.identifier}
                      </span>
                      <span className="truncate">{iss.title}</span>
                    </div>
                    {(alreadyLinked || isSelf) && (
                      <span className="text-[10px] text-muted-foreground">
                        {isSelf ? "Same issue" : `Already ${LINK_LABEL[linkType].outgoing}`}
                      </span>
                    )}
                  </div>
                </PickerItem>
              );
            })}
        </>
      )}
    </PropertyPicker>
  );
}
