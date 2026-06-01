"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, ChevronsUpDown, Loader2, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@multica/ui/components/ui/select";
import {
  feishuProjectBusinessLinesOptions,
  feishuProjectFieldsOptions,
  feishuProjectKeys,
} from "@multica/core/feishu-project/queries";
import { projectListOptions } from "@multica/core/projects";
import { agentListOptions } from "@multica/core/workspace/queries";
import type {
  FeishuProjectBusinessLineNode,
  FeishuProjectIntegration,
} from "@multica/core/types";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

// Each user-edited row in the routes table. business_line_id is the lookup key; we keep
// the parent denormalized so we can save it back to the server without re-fetching the
// tree at save time.
export interface RouteRow {
  businessLineId: string;
  businessLineName: string;
  parentBusinessLineId: string;
  parentBusinessLineName: string;
  projectId: string;
  // Empty string = no fallback. Stored as string so the Select component (which can't
  // hold null) plays well; serialization converts "" → undefined when writing the API.
  fallbackAgentId: string;
}

interface Props {
  workspaceId: string;
  integration: FeishuProjectIntegration | null;
  // Lifted state — parent owns these so the parent's unified Save button can persist
  // the route table together with the integration's business-line field choice.
  fieldKey: string;
  onFieldChanged: (fieldKey: string, fieldName: string) => void;
  rows: RouteRow[];
  setRows: (updater: (prev: RouteRow[]) => RouteRow[]) => void;
  expanded: Record<string, boolean>;
  setExpanded: (updater: (prev: Record<string, boolean>) => Record<string, boolean>) => void;
}

const NO_PROJECT = "__none__";
const NO_AGENT = "__none__";
const NO_FIELD = "__none__";

/**
 * Business-line → project routing UI for a Feishu Project integration.
 *
 * Flow:
 *  1. User picks the "business-line field" (Meego field name varies per space).
 *  2. Once a field is chosen, fetch the 2-level biz-line tree and render as a checkbox tree.
 *  3. For each picked biz-line, pick a workspace-local project.
 *  4. The parent component's unified Save button persists both the field choice (via
 *     updateFeishuProjectIntegration) and the routes (via replaceFeishuProjectRoutes).
 */
export function FeishuProjectRoutingSection({
  workspaceId,
  integration,
  fieldKey,
  onFieldChanged,
  rows,
  setRows,
  expanded,
  setExpanded,
}: Props) {
  const { t } = useT("settings");
  const queryClient = useQueryClient();

  const integrationReady = Boolean(integration?.id && integration.has_plugin_secret);
  const hasFieldKey = fieldKey.trim() !== "";

  const { data: fieldsData, isFetching: fieldsLoading } = useQuery({
    ...feishuProjectFieldsOptions(workspaceId, "issue", integrationReady),
  });
  const fields = fieldsData?.fields ?? [];

  const { data: businessLinesData, isFetching: bizLinesLoading, refetch: refetchBusinessLines } = useQuery({
    ...feishuProjectBusinessLinesOptions(workspaceId, fieldKey, "issue", integrationReady && hasFieldKey),
  });
  const businessLines = businessLinesData?.business_lines ?? [];

  const { data: projects = [] } = useQuery(projectListOptions(workspaceId));
  const { data: agents = [] } = useQuery(agentListOptions(workspaceId));

  // Display-name resolution: prefer the live Meego field list, then the saved name on
  // the integration row (covers the "Meego dropped the field from its response since
  // last save" edge case the user reported — was previously rendering the raw key like
  // `field_b27ba6`), then the raw key as last resort.
  const savedFieldName = integration?.business_line_field_name?.trim() ?? "";
  const liveField = fields.find((f) => f.key === fieldKey);
  const fieldDisplayName = liveField?.name || savedFieldName || fieldKey;
  // Synthesize a SelectItem for the saved key when Meego's field list omits it, so the
  // user can still see + reselect their current choice (otherwise the trigger label looks
  // orphaned vs the dropdown options).
  const syntheticSavedField =
    hasFieldKey && !liveField
      ? { key: fieldKey, name: savedFieldName || fieldKey, type: "(saved)" }
      : null;

  function toggleNode(node: FeishuProjectBusinessLineNode, parent: FeishuProjectBusinessLineNode | null) {
    setRows((prev) => {
      const exists = prev.find((r) => r.businessLineId === node.id);
      if (exists) {
        return prev.filter((r) => r.businessLineId !== node.id);
      }
      return [
        ...prev,
        {
          businessLineId: node.id,
          businessLineName: node.name,
          parentBusinessLineId: parent?.id ?? node.parent_id ?? "",
          parentBusinessLineName: parent?.name ?? node.parent_name ?? "",
          projectId: "",
          fallbackAgentId: "",
        },
      ];
    });
  }

  function setRowProject(bizLineId: string, projectId: string | null) {
    const next = projectId && projectId !== NO_PROJECT ? projectId : "";
    setRows((prev) =>
      prev.map((r) => (r.businessLineId === bizLineId ? { ...r, projectId: next } : r)),
    );
  }

  function setRowFallbackAgent(bizLineId: string, agentId: string | null) {
    const next = agentId && agentId !== NO_AGENT ? agentId : "";
    setRows((prev) =>
      prev.map((r) => (r.businessLineId === bizLineId ? { ...r, fallbackAgentId: next } : r)),
    );
  }

  function removeRow(bizLineId: string) {
    setRows((prev) => prev.filter((r) => r.businessLineId !== bizLineId));
  }

  async function handleRefreshBusinessLines() {
    try {
      // Invalidate to bypass staleTime: Infinity, otherwise refetch is a no-op.
      await queryClient.invalidateQueries({ queryKey: feishuProjectKeys.businessLines(workspaceId, fieldKey, "issue") });
      const r = await refetchBusinessLines();
      if (r.error) {
        toast.error(r.error instanceof Error ? r.error.message : t(($) => $.integrations.feishu_project_business_lines_refresh_failed));
        return;
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.feishu_project_business_lines_refresh_failed));
    }
  }

  function handlePickField(value: string | null) {
    if (!value) return;
    if (value === NO_FIELD) {
      // Clear → disables routing. syncWorkItem treats empty BusinessLineFieldKey
      // as "no routing" and falls back to syncing every item into the workspace
      // without a project (the pre-routing 1:1 behavior).
      onFieldChanged("", "");
    } else if (syntheticSavedField && value === syntheticSavedField.key) {
      onFieldChanged(value, syntheticSavedField.name);
    } else {
      const chosen = fields.find((f) => f.key === value);
      onFieldChanged(value, chosen?.name ?? "");
    }
    // Drop existing routes when the field changes — the biz-line tree may differ.
    setRows(() => []);
    setExpanded(() => ({}));
  }

  if (!integrationReady) {
    return (
      <p className="rounded-md border border-border/70 px-3 py-3 text-xs text-muted-foreground">
        {t(($) => $.integrations.feishu_project_routes_needs_basic)}
      </p>
    );
  }

  const rowsByBizLineId = new Map(rows.map((r) => [r.businessLineId, r] as const));

  return (
    <div className="space-y-4">
      <p className="text-[11px] leading-relaxed text-muted-foreground">
        {t(($) => $.integrations.feishu_project_routing_explainer)}
      </p>

      {/* Field picker row */}
      <label className="block space-y-1.5 text-xs font-medium">
        {t(($) => $.integrations.feishu_project_business_line_field)}
        <FieldPicker
          fields={fields}
          syntheticSavedField={syntheticSavedField}
          fieldKey={fieldKey}
          fieldDisplayName={fieldDisplayName}
          fieldsLoading={fieldsLoading}
          onPick={handlePickField}
          placeholder={t(($) => $.integrations.feishu_project_business_line_field_placeholder)}
          searchPlaceholder={t(($) => $.integrations.feishu_project_business_line_field_search_placeholder)}
          noResultsLabel={t(($) => $.integrations.feishu_project_business_line_field_no_results)}
          loadingLabel={t(($) => $.integrations.feishu_project_fields_loading)}
          emptyLabel={t(($) => $.integrations.feishu_project_fields_empty)}
        />
        <span className="block text-[11px] font-normal text-muted-foreground">
          {t(($) => $.integrations.feishu_project_business_line_field_hint)}
        </span>
      </label>

      {hasFieldKey && (
        <>
          <div className="flex items-center justify-between gap-3 border-b border-border/70 pb-2">
            <p className="text-xs font-medium text-muted-foreground">
              {t(($) => $.integrations.feishu_project_business_lines_tree)}
            </p>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={handleRefreshBusinessLines}
              disabled={bizLinesLoading}
            >
              {bizLinesLoading ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <RefreshCw className="h-3.5 w-3.5" />
              )}
              {t(($) => $.integrations.feishu_project_refresh_business_lines)}
            </Button>
          </div>

          {businessLines.length === 0 ? (
            <p className="rounded-md border border-border/70 px-3 py-3 text-xs text-muted-foreground">
              {bizLinesLoading
                ? t(($) => $.integrations.feishu_project_business_lines_loading)
                : t(($) => $.integrations.feishu_project_business_lines_empty)}
            </p>
          ) : (
            <div className="overflow-hidden rounded-md border border-border/70">
              {businessLines.map((parent) => (
                <BizLineTreeRow
                  key={parent.id || parent.name}
                  node={parent}
                  parent={null}
                  expanded={expanded}
                  setExpanded={setExpanded}
                  rowsByBizLineId={rowsByBizLineId}
                  toggleNode={toggleNode}
                />
              ))}
            </div>
          )}

          {rows.length > 0 && (
            <div className="space-y-2">
              <div className="space-y-0.5">
                <p className="text-xs font-medium">
                  {t(($) => $.integrations.feishu_project_routes_table_title)}
                </p>
                <p className="text-[11px] leading-relaxed text-muted-foreground">
                  {t(($) => $.integrations.feishu_project_routes_fallback_column_hint)}
                </p>
              </div>
              <div className="overflow-hidden rounded-md border border-border/70">
                <div className="grid grid-cols-[1fr_220px_220px_auto] items-center gap-3 border-b border-border/70 bg-muted/30 px-3 py-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  <span className="truncate">
                    {fieldDisplayName
                      ? t(($) => $.integrations.feishu_project_routes_value_column, { field: fieldDisplayName })
                      : t(($) => $.integrations.feishu_project_routes_value_column_generic)}
                  </span>
                  <span>{t(($) => $.integrations.feishu_project_routes_project_column)}</span>
                  <span>{t(($) => $.integrations.feishu_project_routes_fallback_column)}</span>
                  <span />
                </div>
                {rows.map((row) => {
                  const projectChoices = projects.map((p) => ({ id: p.id, title: p.title }));
                  // Only offer active (non-archived) agents in the dropdown — the
                  // workspace query asks the API for archived ones too because other
                  // surfaces (agent management) want to show them, but here picking an
                  // archived agent as a fallback assignee is silly. We still resolve the
                  // trigger label from the full list so an already-saved fallback that
                  // got archived after the fact still renders by name (rather than as
                  // a bare UUID) until the operator re-picks.
                  const agentChoices = agents
                    .filter((a) => !a.archived_at)
                    .map((a) => ({ id: a.id, name: a.name }));
                  const fallbackName = row.fallbackAgentId
                    ? (agents.find((a) => a.id === row.fallbackAgentId)?.name ?? row.fallbackAgentId)
                    : "";
                  return (
                    <div
                      key={row.businessLineId}
                      className="grid grid-cols-[1fr_220px_220px_auto] items-center gap-3 border-b border-border/70 px-3 py-2 last:border-b-0"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-xs font-medium">
                          {row.businessLineName || row.businessLineId}
                        </p>
                        {row.parentBusinessLineName && (
                          <p className="truncate text-[11px] text-muted-foreground">
                            {row.parentBusinessLineName} / {row.businessLineName || row.businessLineId}
                          </p>
                        )}
                      </div>
                      <Select
                        value={row.projectId || NO_PROJECT}
                        onValueChange={(v) => setRowProject(row.businessLineId, v)}
                      >
                        <SelectTrigger size="sm" className="w-full">
                          <span className="flex-1 truncate text-left">
                            {row.projectId
                              ? (projectChoices.find((p) => p.id === row.projectId)?.title ?? row.projectId)
                              : t(($) => $.integrations.feishu_project_routes_pick_project)}
                          </span>
                        </SelectTrigger>
                        <SelectContent align="start">
                          {projectChoices.length === 0 && (
                            <div className="px-2 py-1.5 text-xs text-muted-foreground">
                              {t(($) => $.integrations.feishu_project_routes_no_projects)}
                            </div>
                          )}
                          {projectChoices.map((p) => (
                            <SelectItem key={p.id} value={p.id}>
                              {p.title}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Select
                        value={row.fallbackAgentId || NO_AGENT}
                        onValueChange={(v) => setRowFallbackAgent(row.businessLineId, v)}
                      >
                        <SelectTrigger size="sm" className="w-full">
                          <span className="flex-1 truncate text-left">
                            {row.fallbackAgentId
                              ? fallbackName
                              : t(($) => $.integrations.feishu_project_routes_fallback_agent_placeholder)}
                          </span>
                        </SelectTrigger>
                        <SelectContent align="start">
                          <SelectItem value={NO_AGENT}>
                            {t(($) => $.integrations.feishu_project_routes_fallback_agent_none)}
                          </SelectItem>
                          {agentChoices.length === 0 && (
                            <div className="px-2 py-1.5 text-xs text-muted-foreground">
                              {t(($) => $.integrations.feishu_project_routes_no_agents)}
                            </div>
                          )}
                          {agentChoices.map((a) => (
                            <SelectItem key={a.id} value={a.id}>
                              {a.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => removeRow(row.businessLineId)}
                        aria-label={t(($) => $.integrations.feishu_project_routes_remove)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}

interface RowProps {
  node: FeishuProjectBusinessLineNode;
  parent: FeishuProjectBusinessLineNode | null;
  expanded: Record<string, boolean>;
  setExpanded: (updater: (prev: Record<string, boolean>) => Record<string, boolean>) => void;
  rowsByBizLineId: Map<string, RouteRow>;
  toggleNode: (node: FeishuProjectBusinessLineNode, parent: FeishuProjectBusinessLineNode | null) => void;
}

function BizLineTreeRow({ node, parent, expanded, setExpanded, rowsByBizLineId, toggleNode }: RowProps) {
  const hasChildren = (node.children?.length ?? 0) > 0;
  const isOpen = expanded[node.id];
  const checked = rowsByBizLineId.has(node.id);
  const depth = parent ? 1 : 0;

  return (
    <>
      <div
        className="flex items-center gap-2 border-b border-border/70 px-3 py-1.5 last:border-b-0"
        style={{ paddingLeft: `${0.75 + depth * 1.25}rem` }}
      >
        {hasChildren ? (
          <button
            type="button"
            className="flex h-5 w-5 items-center justify-center rounded hover:bg-muted"
            onClick={() => setExpanded((prev) => ({ ...prev, [node.id]: !prev[node.id] }))}
            aria-label={isOpen ? "collapse" : "expand"}
          >
            {isOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
        ) : (
          <span className="inline-block h-5 w-5" />
        )}
        <input
          type="checkbox"
          className="h-3.5 w-3.5"
          checked={checked}
          onChange={() => toggleNode(node, parent)}
        />
        <span className="truncate text-xs">
          {node.name || node.id}
        </span>
      </div>
      {hasChildren && isOpen && (
        <>
          {(node.children ?? []).map((child) => (
            <BizLineTreeRow
              key={child.id || child.name}
              node={child}
              parent={node}
              expanded={expanded}
              setExpanded={setExpanded}
              rowsByBizLineId={rowsByBizLineId}
              toggleNode={toggleNode}
            />
          ))}
        </>
      )}
    </>
  );
}

type FieldOption = { key: string; name: string; type: string };

// Searchable Popover+Command picker. Replaces the plain <Select> we used before — Meego's
// /field/all returns ~50 fields per work-item type, which made scrolling painful. Matches
// on field name, field_key, and pinyin (for Chinese field names like "BUG提单助手").
// Preserves the syntheticSavedField, loading, and empty states the old Select had.
function FieldPicker({
  fields,
  syntheticSavedField,
  fieldKey,
  fieldDisplayName,
  fieldsLoading,
  onPick,
  placeholder,
  searchPlaceholder,
  noResultsLabel,
  loadingLabel,
  emptyLabel,
}: {
  fields: FieldOption[];
  syntheticSavedField: FieldOption | null;
  fieldKey: string;
  fieldDisplayName: string;
  fieldsLoading: boolean;
  onPick: (value: string | null) => void;
  placeholder: string;
  searchPlaceholder: string;
  noResultsLabel: string;
  loadingLabel: string;
  emptyLabel: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const hasFieldKey = fieldKey.trim() !== "";
  const triggerDisabled = fieldsLoading && !syntheticSavedField;

  const visibleFields = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return fields;
    return fields.filter(
      (f) =>
        f.name.toLowerCase().includes(q) ||
        f.key.toLowerCase().includes(q) ||
        matchesPinyin(f.name, q),
    );
  }, [fields, query]);

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery("");
      }}
    >
      <PopoverTrigger
        disabled={triggerDisabled}
        className="flex h-8 w-full items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent px-2.5 text-sm whitespace-nowrap transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 dark:hover:bg-input/50"
      >
        <span className={`min-w-0 flex-1 truncate text-left ${hasFieldKey ? "" : "text-muted-foreground"}`}>
          {hasFieldKey ? fieldDisplayName : placeholder}
        </span>
        <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={4} className="w-[var(--anchor-width)] p-0">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={searchPlaceholder}
            value={query}
            onValueChange={setQuery}
          />
          <CommandList className="max-h-64">
            {fields.length === 0 && !syntheticSavedField ? (
              <div className="px-2 py-3 text-center text-xs text-muted-foreground">
                {fieldsLoading ? loadingLabel : emptyLabel}
              </div>
            ) : (
              <>
                {visibleFields.length === 0 && !syntheticSavedField && (
                  <CommandEmpty>{noResultsLabel}</CommandEmpty>
                )}
                {/* "Clear" entry — picking it sends "__none__" back to onPick, which
                    handlePickField treats as "disable routing". Without this the
                    operator has no way to undo a field choice (label-sync's picker
                    has the same affordance). */}
                <CommandGroup>
                  <CommandItem
                    value="__none__"
                    onSelect={(value) => {
                      onPick(value);
                      setOpen(false);
                    }}
                  >
                    <span className="text-muted-foreground">{placeholder}</span>
                  </CommandItem>
                </CommandGroup>
                {syntheticSavedField && (
                  <CommandGroup>
                    <CommandItem
                      key={syntheticSavedField.key}
                      value={syntheticSavedField.key}
                      onSelect={(value) => {
                        onPick(value);
                        setOpen(false);
                      }}
                      className="flex items-center gap-2"
                    >
                      <span className="min-w-0 flex-1 truncate">{syntheticSavedField.name}</span>
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                        {syntheticSavedField.key}
                      </span>
                    </CommandItem>
                  </CommandGroup>
                )}
                <CommandGroup>
                  {visibleFields.map((f) => (
                    <CommandItem
                      key={f.key}
                      value={f.key}
                      onSelect={(value) => {
                        onPick(value);
                        setOpen(false);
                      }}
                      className="flex items-center gap-2"
                    >
                      <span className="min-w-0 flex-1 truncate">{f.name}</span>
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                        {f.key}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
