import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Battery, Check, Clock, Edit3, ListPlus, Play, Plus, Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { useCreateIssueTypeMutation, useIssueTypesQuery } from "@/features/issues/hooks";
import type { Issue, IssueType, PlanItem, PlanItemStatus } from "@/shared/types";
import {
  useCreatePlanItemMutation,
  usePlanCandidatesQuery,
  usePlanQuery,
  useStartPlanItemFocusMutation,
  useUpdatePlanItemMutation,
  useUpsertPlanMutation,
} from "../hooks/use-plan";

type PlanSearch = {
  date?: string;
};

const statusOptions: PlanItemStatus[] = ["planned", "in_progress", "progressed", "done", "skipped"];

const loadProfileLabels: Record<string, string> = {
  deep_work: "Deep work",
  light_work: "Light work",
  recovery: "Recovery",
  neutral: "Neutral",
};

function formatMinutes(value: number | null): string {
  if (!value) return "No estimate";
  if (value < 60) return `${value}m`;
  const hours = Math.floor(value / 60);
  const minutes = value % 60;
  return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
}

function formatActual(seconds: number): string {
  if (seconds <= 0) return "No time";
  return formatMinutes(Math.round(seconds / 60));
}

function issueTypeLabel(issueTypeId: string | null | undefined, issueTypes: IssueType[]): string {
  return issueTypes.find((item) => item.id === issueTypeId)?.name ?? "Task";
}

function issueTypeProfile(issueTypeId: string | null | undefined, issueTypes: IssueType[]): string {
  const profile = issueTypes.find((item) => item.id === issueTypeId)?.load_profile ?? "neutral";
  return loadProfileLabels[profile] ?? "Neutral";
}

function defaultIssueTypeId(issueTypes: IssueType[]): string | null {
  return issueTypes.find((item) => item.key === "task")?.id ?? issueTypes[0]?.id ?? null;
}

function PlanItemRow({
  item,
  issueTypes,
  onEdit,
  onStatus,
  onStart,
}: {
  item: PlanItem;
  issueTypes: IssueType[];
  onEdit: () => void;
  onStatus: (status: PlanItemStatus) => void;
  onStart: () => void;
}) {
  const typeId = item.issue_type_id ?? item.suggested_issue_type_id;
  return (
    <div className="rounded-lg border bg-background p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="truncate text-sm font-medium">{item.title_snapshot}</h3>
            <Badge variant="secondary">{item.status}</Badge>
            <Badge variant="outline">{issueTypeLabel(typeId, issueTypes)}</Badge>
            {item.issue_id && <Badge variant="outline">Issue linked</Badge>}
          </div>
          {item.note && <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{item.note}</p>}
          <div className="mt-2 flex flex-wrap gap-3 text-xs text-muted-foreground">
            <span className="inline-flex items-center gap-1">
              <Clock className="size-3" />
              {formatMinutes(item.estimated_minutes)}
            </span>
            <span>{formatActual(item.actual_seconds)}</span>
            <span>{issueTypeProfile(typeId, issueTypes)}</span>
          </div>
        </div>
        <div className="flex shrink-0 gap-1">
          <Button size="icon" variant="ghost" onClick={onEdit} aria-label="Edit plan item">
            <Edit3 className="size-4" />
          </Button>
          <Button size="icon" variant="ghost" onClick={onStart} aria-label="Start focus">
            <Play className="size-4" />
          </Button>
          <Button size="icon" variant="ghost" onClick={() => onStatus("done")} aria-label="Mark done">
            <Check className="size-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function CandidateRow({
  issue,
  issueTypes,
  onAdd,
}: {
  issue: Issue;
  issueTypes: IssueType[];
  onAdd: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border bg-background p-3">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="truncate text-sm font-medium">{issue.title}</h3>
          <Badge variant="outline">{issueTypeLabel(issue.issue_type_id, issueTypes)}</Badge>
          <Badge variant="secondary">{issue.priority}</Badge>
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          {issue.identifier} · {issue.status} · {issueTypeProfile(issue.issue_type_id, issueTypes)}
        </p>
      </div>
      <Button size="sm" variant="outline" onClick={onAdd}>
        <ListPlus className="size-4" />
        Add
      </Button>
    </div>
  );
}

export function PlanPage() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as PlanSearch;
  const date = search.date ?? "today";
  const [newTitle, setNewTitle] = useState("");
  const [newEstimate, setNewEstimate] = useState("");
  const [newIssueTypeId, setNewIssueTypeId] = useState<string>("");
  const [newTypeName, setNewTypeName] = useState("");
  const [newTypeProfile, setNewTypeProfile] = useState("neutral");
  const [selectedIssueTypeId, setSelectedIssueTypeId] = useState<string>("");
  const [capacityMinutes, setCapacityMinutes] = useState("");
  const [capacityNote, setCapacityNote] = useState("");
  const [energyNote, setEnergyNote] = useState("");
  const [selectedItemId, setSelectedItemId] = useState<string | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const [editEstimate, setEditEstimate] = useState("");
  const [editNote, setEditNote] = useState("");
  const [editStatus, setEditStatus] = useState<PlanItemStatus>("planned");
  const [editIssueTypeId, setEditIssueTypeId] = useState("");
  const planQuery = usePlanQuery(date);
  const issueTypesQuery = useIssueTypesQuery();
  const candidatesQuery = usePlanCandidatesQuery(date, selectedIssueTypeId || undefined);
  const plan = planQuery.data;
  const issueTypes = issueTypesQuery.data ?? [];
  const createIssueType = useCreateIssueTypeMutation();
  const upsertPlan = useUpsertPlanMutation(date);
  const createItem = useCreatePlanItemMutation(date, plan?.id ?? "");
  const updateItem = useUpdatePlanItemMutation(date);
  const startFocus = useStartPlanItemFocusMutation(date);
  const selectedItem = plan?.items.find((item) => item.id === selectedItemId) ?? null;
  const itemCountByStatus = useMemo(() => {
    return statusOptions.reduce<Record<PlanItemStatus, number>>((acc, status) => {
      acc[status] = plan?.items.filter((item) => item.status === status).length ?? 0;
      return acc;
    }, { planned: 0, in_progress: 0, progressed: 0, done: 0, skipped: 0 });
  }, [plan?.items]);
  const totals = useMemo(() => {
    const estimated = plan?.items.reduce((sum, item) => sum + (item.estimated_minutes ?? 0), 0) ?? 0;
    const actual = plan?.items.reduce((sum, item) => sum + item.actual_seconds, 0) ?? 0;
    return { estimated, actual };
  }, [plan?.items]);

  useEffect(() => {
    if (!newIssueTypeId) {
      setNewIssueTypeId(defaultIssueTypeId(issueTypes) ?? "");
    }
  }, [issueTypes, newIssueTypeId]);

  useEffect(() => {
    setCapacityMinutes(plan?.capacity_minutes ? String(plan.capacity_minutes) : "");
    setCapacityNote(plan?.capacity_note ?? "");
    setEnergyNote(plan?.energy_note ?? "");
  }, [plan?.id, plan?.capacity_minutes, plan?.capacity_note, plan?.energy_note]);

  useEffect(() => {
    if (!selectedItem) return;
    setEditTitle(selectedItem.title_snapshot);
    setEditEstimate(selectedItem.estimated_minutes ? String(selectedItem.estimated_minutes) : "");
    setEditNote(selectedItem.note);
    setEditStatus(selectedItem.status);
    setEditIssueTypeId(selectedItem.issue_type_id ?? selectedItem.suggested_issue_type_id ?? defaultIssueTypeId(issueTypes) ?? "");
  }, [selectedItem, issueTypes]);

  const setDate = (nextDate: string) => {
    navigate({ to: "/plan", search: { date: nextDate } });
  };

  const savePlanSignals = (updates: {
    energyLevel?: number | null;
    recoveryNeed?: boolean;
    capacity?: string;
    capacityText?: string;
    energyText?: string;
  }) => {
    upsertPlan.mutate({
      date,
      energy_level: updates.energyLevel ?? plan?.energy_level ?? null,
      recovery_need: updates.recoveryNeed ?? plan?.recovery_need ?? false,
      capacity_minutes: updates.capacity !== undefined
        ? (updates.capacity ? Number(updates.capacity) : null)
        : plan?.capacity_minutes ?? null,
      capacity_note: updates.capacityText ?? plan?.capacity_note ?? null,
      energy_note: updates.energyText ?? plan?.energy_note ?? null,
    });
  };

  const addManualItem = () => {
    if (!plan || !newTitle.trim()) return;
    createItem.mutate({
      title: newTitle.trim(),
      estimated_minutes: newEstimate ? Number(newEstimate) : null,
      suggested_issue_type_id: newIssueTypeId || defaultIssueTypeId(issueTypes),
    }, {
      onSuccess: () => {
        setNewTitle("");
        setNewEstimate("");
      },
    });
  };

  const addIssueType = () => {
    const name = newTypeName.trim();
    if (!name) return;
    createIssueType.mutate({
      key: name.toLowerCase().replace(/\s+/g, "-"),
      name,
      load_profile: newTypeProfile,
      color: "gray",
      icon: "circle",
      position: issueTypes.length * 10 + 100,
    }, {
      onSuccess: () => setNewTypeName(""),
    });
  };

  const saveSelectedItem = () => {
    if (!selectedItem) return;
    updateItem.mutate({
      itemId: selectedItem.id,
      body: {
        title: editTitle.trim() || selectedItem.title_snapshot,
        estimated_minutes: editEstimate ? Number(editEstimate) : null,
        note: editNote,
        status: editStatus,
        suggested_issue_type_id: selectedItem.issue_id ? undefined : editIssueTypeId || null,
      },
    }, {
      onSuccess: () => setSelectedItemId(null),
    });
  };

  if (planQuery.isLoading) {
    return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  }

  if (planQuery.isError) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="max-w-md rounded-lg border bg-background p-4 text-center">
          <h1 className="text-base font-medium">Plan is unavailable</h1>
          <p className="mt-2 text-sm text-muted-foreground">Refresh after the backend is running with the latest Plan API.</p>
          <Button className="mt-4" onClick={() => planQuery.refetch()}>Retry</Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="border-b px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold">Plan</h1>
            <p className="text-sm text-muted-foreground">Issue execution and energy planning</p>
          </div>
          <div className="flex rounded-lg border p-1">
            <Button size="sm" variant={date === "today" ? "default" : "ghost"} onClick={() => setDate("today")}>Today</Button>
            <Button size="sm" variant={date === "tomorrow" ? "default" : "ghost"} onClick={() => setDate("tomorrow")}>Tomorrow</Button>
          </div>
        </div>
      </div>

      <div className="grid min-h-0 flex-1 gap-4 overflow-auto p-4 lg:grid-cols-[minmax(0,1fr)_380px]">
        <section className="min-w-0 space-y-4">
          <div className="grid gap-3 rounded-lg border bg-muted/20 p-3 sm:grid-cols-4">
            <div>
              <p className="text-xs text-muted-foreground">Estimated</p>
              <p className="text-sm font-medium">{formatMinutes(totals.estimated)}</p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">Actual</p>
              <p className="text-sm font-medium">{formatActual(totals.actual)}</p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">Done</p>
              <p className="text-sm font-medium">{itemCountByStatus.done}/{plan?.items.length ?? 0}</p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">Energy</p>
              <div className="mt-1 flex items-center gap-2">
                <Battery className="size-4 text-muted-foreground" />
                <select
                  className="h-8 rounded-md border bg-background px-2 text-sm"
                  value={plan?.energy_level ?? ""}
                  onChange={(event) => savePlanSignals({ energyLevel: event.target.value ? Number(event.target.value) : null })}
                >
                  <option value="">Not set</option>
                  {[1, 2, 3, 4, 5].map((level) => <option key={level} value={level}>{level}</option>)}
                </select>
              </div>
            </div>
          </div>

          <div className="grid gap-2 rounded-lg border bg-background p-3 md:grid-cols-[minmax(0,1fr)_110px_150px_auto]">
            <Input value={newTitle} onChange={(event) => setNewTitle(event.target.value)} placeholder="Add plan item" />
            <Input value={newEstimate} onChange={(event) => setNewEstimate(event.target.value)} placeholder="Min" type="number" />
            <select
              className="h-9 rounded-md border bg-background px-2 text-sm"
              value={newIssueTypeId}
              onChange={(event) => setNewIssueTypeId(event.target.value)}
            >
              {issueTypes.map((issueType) => (
                <option key={issueType.id} value={issueType.id}>{issueType.name}</option>
              ))}
            </select>
            <Button onClick={addManualItem} disabled={!plan || !newTitle.trim() || createItem.isPending}>
              <Plus className="size-4" />
              Add
            </Button>
          </div>

          <div className="space-y-2">
            {plan?.items.length === 0 && (
              <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
                No plan items. Add a manual item or choose candidates from the right panel.
              </div>
            )}
            {plan?.items.map((item) => (
              <PlanItemRow
                key={item.id}
                item={item}
                issueTypes={issueTypes}
                onEdit={() => setSelectedItemId(item.id)}
                onStatus={(status) => updateItem.mutate({ itemId: item.id, body: { status } })}
                onStart={() => startFocus.mutate({ itemId: item.id, issueId: item.issue_id, title: item.title_snapshot })}
              />
            ))}
          </div>
        </section>

        <aside className="min-w-0 space-y-4">
          <div className="rounded-lg border bg-background p-3">
            <div className="mb-3 flex items-center gap-2">
              <Search className="size-4 text-muted-foreground" />
              <h2 className="text-sm font-medium">Candidates</h2>
            </div>
            <select
              className="mb-3 h-9 w-full rounded-md border bg-background px-2 text-sm"
              value={selectedIssueTypeId}
              onChange={(event) => setSelectedIssueTypeId(event.target.value)}
            >
              <option value="">All issue types</option>
              {issueTypes.map((issueType) => (
                <option key={issueType.id} value={issueType.id}>{issueType.name} · {loadProfileLabels[issueType.load_profile]}</option>
              ))}
            </select>
            <div className="space-y-2">
              {candidatesQuery.data?.issues.map((issue) => (
                <CandidateRow
                  key={issue.id}
                  issue={issue}
                  issueTypes={issueTypes}
                  onAdd={() => {
                    if (!plan) return;
                    createItem.mutate({
                      issue_id: issue.id,
                      title: issue.title,
                      suggested_issue_type_id: issue.issue_type_id,
                    });
                  }}
                />
              ))}
              {candidatesQuery.data?.issues.length === 0 && (
                <p className="py-6 text-center text-sm text-muted-foreground">No candidates</p>
              )}
            </div>
          </div>

          <div className="rounded-lg border bg-background p-3">
            <h2 className="mb-3 text-sm font-medium">Capacity</h2>
            <div className="space-y-3">
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={plan?.recovery_need ?? false}
                  onChange={(event) => savePlanSignals({ recoveryNeed: event.target.checked })}
                />
                Recovery needed
              </label>
              <Input
                value={capacityMinutes}
                onChange={(event) => setCapacityMinutes(event.target.value)}
                onBlur={() => savePlanSignals({ capacity: capacityMinutes })}
                placeholder="Capacity minutes"
                type="number"
              />
              <Textarea
                value={energyNote}
                onChange={(event) => setEnergyNote(event.target.value)}
                onBlur={() => savePlanSignals({ energyText: energyNote })}
                placeholder="Energy note"
              />
              <Textarea
                value={capacityNote}
                onChange={(event) => setCapacityNote(event.target.value)}
                onBlur={() => savePlanSignals({ capacityText: capacityNote })}
                placeholder="Capacity note"
              />
            </div>
          </div>

          <div className="rounded-lg border bg-background p-3">
            <h2 className="mb-3 text-sm font-medium">Issue types</h2>
            <div className="mb-3 flex flex-wrap gap-2">
              {issueTypes.map((issueType) => (
                <Badge key={issueType.id} variant="outline">{issueType.name}</Badge>
              ))}
            </div>
            <div className="grid gap-2">
              <Input value={newTypeName} onChange={(event) => setNewTypeName(event.target.value)} placeholder="New type" />
              <select
                className="h-9 rounded-md border bg-background px-2 text-sm"
                value={newTypeProfile}
                onChange={(event) => setNewTypeProfile(event.target.value)}
              >
                <option value="neutral">Neutral</option>
                <option value="deep_work">Deep work</option>
                <option value="light_work">Light work</option>
                <option value="recovery">Recovery</option>
              </select>
              <Button onClick={addIssueType} disabled={!newTypeName.trim() || createIssueType.isPending}>Add type</Button>
            </div>
          </div>
        </aside>
      </div>

      <Sheet open={!!selectedItem} onOpenChange={(open) => { if (!open) setSelectedItemId(null); }}>
        <SheetContent className="w-full sm:max-w-md">
          <SheetHeader>
            <SheetTitle>Plan item</SheetTitle>
            <SheetDescription>Edit the planned work without leaving the Plan page.</SheetDescription>
          </SheetHeader>
          {selectedItem && (
            <div className="grid gap-4 px-4">
              <div className="grid gap-2">
                <label className="text-sm font-medium" htmlFor="plan-item-title">Title</label>
                <Input id="plan-item-title" value={editTitle} onChange={(event) => setEditTitle(event.target.value)} />
              </div>
              <div className="grid gap-2">
                <label className="text-sm font-medium" htmlFor="plan-item-note">Note</label>
                <Textarea id="plan-item-note" value={editNote} onChange={(event) => setEditNote(event.target.value)} />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-2">
                  <label className="text-sm font-medium" htmlFor="plan-item-estimate">Estimate</label>
                  <Input id="plan-item-estimate" value={editEstimate} onChange={(event) => setEditEstimate(event.target.value)} type="number" />
                </div>
                <div className="grid gap-2">
                  <label className="text-sm font-medium" htmlFor="plan-item-status">Status</label>
                  <select
                    id="plan-item-status"
                    className="h-9 rounded-md border bg-background px-2 text-sm"
                    value={editStatus}
                    onChange={(event) => setEditStatus(event.target.value as PlanItemStatus)}
                  >
                    {statusOptions.map((status) => <option key={status} value={status}>{status}</option>)}
                  </select>
                </div>
              </div>
              <div className="grid gap-2">
                <label className="text-sm font-medium" htmlFor="plan-item-type">Issue type</label>
                <select
                  id="plan-item-type"
                  className="h-9 rounded-md border bg-background px-2 text-sm disabled:opacity-60"
                  value={editIssueTypeId}
                  disabled={!!selectedItem.issue_id}
                  onChange={(event) => setEditIssueTypeId(event.target.value)}
                >
                  {issueTypes.map((issueType) => (
                    <option key={issueType.id} value={issueType.id}>{issueType.name} · {loadProfileLabels[issueType.load_profile]}</option>
                  ))}
                </select>
                {selectedItem.issue_id && (
                  <p className="text-xs text-muted-foreground">Linked issue items use the issue's current type.</p>
                )}
              </div>
            </div>
          )}
          <SheetFooter>
            <Button onClick={saveSelectedItem} disabled={!selectedItem || updateItem.isPending}>Save changes</Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </div>
  );
}
