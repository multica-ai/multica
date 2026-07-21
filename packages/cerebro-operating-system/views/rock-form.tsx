import { useEffect, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueListOptions } from "@multica/core/issues/queries";
import { projectListOptions } from "@multica/core/projects";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { OperatingPeriod, Rock, StrategyItem, Terminology } from "../core/types";
import { formatPeriodRange, nextPeriodInput } from "../core/periods";
import { goalTypesOptions, useSavePeriod, useSaveRock } from "../core/queries";
import { SearchSelect } from "./search-select";

interface RockFormProps {
  wsId: string;
  terminology: Terminology;
  periods: OperatingPeriod[];
  strategyItems: StrategyItem[];
  rock?: Rock;
  onSaved: () => void;
  onCancel: () => void;
}

const HEALTH_OPTIONS = [
  { value: "unset", label: "Not reported" },
  { value: "on_track", label: "On track" },
  { value: "at_risk", label: "At risk" },
  { value: "off_track", label: "Off track" },
];

export function RockForm({ wsId, terminology, periods, strategyItems, rock, onSaved, onCancel }: RockFormProps) {
  const projects = useQuery(projectListOptions(wsId));
  const issues = useQuery(issueListOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const goalTypes = useQuery(goalTypesOptions(wsId));
  const save = useSaveRock(wsId);
  const savePeriod = useSavePeriod(wsId);
  const [title, setTitle] = useState(rock?.title ?? "");
  const [description, setDescription] = useState(rock?.description ?? "");
  const [periodId, setPeriodId] = useState(rock?.period_id ?? "");
  const [goalTypeId, setGoalTypeId] = useState(rock?.goal_type_id ?? "");
  const [owner, setOwner] = useState(rock?.owner_id ? `${rock.owner_type}:${rock.owner_id}` : "");
  const [confidence, setConfidence] = useState(rock?.confidence ?? 50);
  const [reportedHealth, setReportedHealth] = useState(rock?.reported_health === "unknown" ? "unset" : rock?.reported_health ?? "unset");
  const [projectIds, setProjectIds] = useState(rock?.projects.map((project) => project.id) ?? []);
  const [issueIds, setIssueIds] = useState(rock?.issues.filter((issue) => !issue.project_id).map((issue) => issue.id) ?? []);
  const [strategyItemId, setStrategyItemId] = useState(rock?.strategy_item_id ?? "");

  useEffect(() => {
    if (!periodId && periods[0]) setPeriodId(periods[0].id);
  }, [periodId, periods]);

  function planNextPeriod() {
    savePeriod.mutate({ input: nextPeriodInput(periods) }, { onSuccess: (period) => { if (period) setPeriodId(period.id); } });
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const [ownerType, ownerId] = owner.split(":");
    save.mutate({
      id: rock?.id,
      input: {
        title: title.trim(),
        description: description.trim(),
        period_id: periodId,
        goal_type_id: goalTypeId || undefined,
        confidence,
        reported_health: reportedHealth,
        owner_type: ownerId ? ownerType as "member" | "agent" : undefined,
        owner_id: ownerId || undefined,
        project_ids: projectIds,
        issue_ids: issueIds,
        strategy_item_id: strategyItemId || undefined,
      },
    }, { onSuccess: onSaved });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-2">
      <div className="sm:col-span-2">
        <h2 className="text-lg font-semibold">{rock ? `Edit ${terminology.rock}` : `New ${terminology.rock}`}</h2>
        <p className="text-sm text-muted-foreground">Define the outcome first. Connections to Projects and Issues are optional.</p>
      </div>
      <label className="grid gap-1 text-sm sm:col-span-2">{terminology.rock} title
        <input aria-label={`${terminology.rock} title`} required value={title} onChange={(event) => setTitle(event.target.value)} className="h-10 rounded-md border bg-background px-3" />
      </label>
      <label className="grid gap-1 text-sm sm:col-span-2">Description
        <textarea aria-label="Description" value={description} onChange={(event) => setDescription(event.target.value)} rows={3} className="rounded-md border bg-background px-3 py-2" />
      </label>
      <div className="grid gap-1 text-sm"><span>Period</span>
        <SearchSelect label="Period" options={periods.map((period) => ({ value: period.id, label: period.name, hint: formatPeriodRange(period) }))} value={periodId} onChange={setPeriodId} actionLabel="Plan next period" onAction={planNextPeriod} placeholder="Select period" />
      </div>
      <div className="grid gap-1 text-sm"><span>Type</span>
        <SearchSelect label="Type" options={(goalTypes.data?.goal_types ?? []).map((goalType) => ({ value: goalType.id, label: goalType.name, hint: goalType.scope_label || undefined, color: goalType.color || undefined }))} value={goalTypeId} onChange={setGoalTypeId} clearLabel="No type" placeholder="No type" />
      </div>
      <div className="grid gap-1 text-sm"><span>Owner</span>
        <SearchSelect label="Owner" options={[...(members.data ?? []).map((member) => ({ value: `member:${member.id}`, label: member.name, group: "Members" })), ...(agents.data ?? []).map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" }))]} value={owner} onChange={setOwner} clearLabel="No owner" placeholder="No owner" />
      </div>
      <label className="grid gap-1 text-sm">Confidence
        <input aria-label="Confidence" type="number" min={0} max={100} value={confidence} onChange={(event) => setConfidence(Number(event.target.value))} className="h-10 rounded-md border bg-background px-3" />
      </label>
      <div className="grid gap-1 text-sm"><span>Reported health</span>
        <SearchSelect label="Reported health" options={HEALTH_OPTIONS} value={reportedHealth} onChange={(value) => setReportedHealth(value as "unset" | "on_track" | "at_risk" | "off_track")} placeholder="Not reported" />
      </div>
      <div className="grid gap-1 text-sm sm:col-span-2"><span>{terminology.strategy} connection</span>
        <SearchSelect label="Strategy connection" options={strategyItems.filter((item) => item.state === "active").map((item) => ({ value: item.id, label: item.title }))} value={strategyItemId} onChange={setStrategyItemId} clearLabel={`No ${terminology.strategy} connection`} placeholder={`No ${terminology.strategy} connection`} />
      </div>
      <div className="grid gap-1.5 text-sm"><span>Projects <span className="text-xs text-muted-foreground">Optional · select multiple</span></span>
        <SearchSelect label="Projects" multiple options={(projects.data ?? []).map((project) => ({ value: project.id, label: project.title }))} values={projectIds} onValuesChange={setProjectIds} placeholder="No projects linked" />
        {projectIds.length > 0 && (
          <ul aria-label="Linked projects" className="flex flex-wrap gap-1.5">
            {projectIds.map((id) => {
              const label = (projects.data ?? []).find((project) => project.id === id)?.title ?? id;
              return <li key={id}><button type="button" aria-label={`Remove project ${label}`} onClick={() => setProjectIds(projectIds.filter((value) => value !== id))} className="flex max-w-full items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-xs"><span className="truncate">{label}</span><span aria-hidden className="text-muted-foreground">×</span></button></li>;
            })}
          </ul>
        )}
      </div>
      <div className="grid gap-1.5 text-sm"><span>Issues <span className="text-xs text-muted-foreground">Optional · select multiple</span></span>
        <SearchSelect label="Issues" multiple options={(issues.data ?? []).map((issue) => ({ value: issue.id, label: `${issue.identifier} · ${issue.title}` }))} values={issueIds} onValuesChange={setIssueIds} placeholder="No issues linked" />
        {issueIds.length > 0 && (
          <ul aria-label="Linked issues" className="flex flex-wrap gap-1.5">
            {issueIds.map((id) => {
              const issue = (issues.data ?? []).find((candidate) => candidate.id === id);
              const label = issue ? `${issue.identifier} · ${issue.title}` : id;
              return <li key={id}><button type="button" aria-label={`Remove issue ${label}`} onClick={() => setIssueIds(issueIds.filter((value) => value !== id))} className="flex max-w-full items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-xs"><span className="truncate">{label}</span><span aria-hidden className="text-muted-foreground">×</span></button></li>;
            })}
          </ul>
        )}
      </div>
      {save.isError && <p role="alert" className="text-sm text-destructive sm:col-span-2">The {terminology.rock.toLowerCase()} could not be saved.</p>}
      <div className="flex justify-end gap-2 sm:col-span-2">
        <button type="button" onClick={onCancel} className="h-10 rounded-md border px-4 text-sm font-medium">Cancel</button>
        <button disabled={save.isPending} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Save {terminology.rock}</button>
      </div>
    </form>
  );
}
