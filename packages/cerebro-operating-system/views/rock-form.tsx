import { useEffect, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueListOptions } from "@multica/core/issues/queries";
import { projectListOptions } from "@multica/core/projects";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { OperatingPeriod, Rock, StrategyItem, Terminology } from "../core/types";
import { useSaveRock } from "../core/queries";

interface RockFormProps {
  wsId: string;
  terminology: Terminology;
  periods: OperatingPeriod[];
  strategyItems: StrategyItem[];
  rock?: Rock;
  onSaved: () => void;
  onCancel: () => void;
}

function selectedValues(element: HTMLSelectElement) {
  return Array.from(element.selectedOptions, (option) => option.value);
}

export function RockForm({ wsId, terminology, periods, strategyItems, rock, onSaved, onCancel }: RockFormProps) {
  const projects = useQuery(projectListOptions(wsId));
  const issues = useQuery(issueListOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const save = useSaveRock(wsId);
  const [title, setTitle] = useState(rock?.title ?? "");
  const [description, setDescription] = useState(rock?.description ?? "");
  const [periodId, setPeriodId] = useState(rock?.period_id ?? "");
  const [owner, setOwner] = useState(rock?.owner_id ? `${rock.owner_type}:${rock.owner_id}` : "");
  const [confidence, setConfidence] = useState(rock?.confidence ?? 50);
  const [reportedHealth, setReportedHealth] = useState(rock?.reported_health === "unknown" ? "unset" : rock?.reported_health ?? "unset");
  const [projectIds, setProjectIds] = useState(rock?.projects.map((project) => project.id) ?? []);
  const [issueIds, setIssueIds] = useState(rock?.issues.filter((issue) => !issue.project_id).map((issue) => issue.id) ?? []);
  const [strategyItemId, setStrategyItemId] = useState(rock?.strategy_item_id ?? "");

  useEffect(() => {
    if (!periodId && periods[0]) setPeriodId(periods[0].id);
  }, [periodId, periods]);

  function submit(event: FormEvent) {
    event.preventDefault();
    const [ownerType, ownerId] = owner.split(":");
    save.mutate({
      id: rock?.id,
      input: {
        title: title.trim(),
        description: description.trim(),
        period_id: periodId,
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
      <label className="grid gap-1 text-sm">Period
        <select aria-label="Period" required value={periodId} onChange={(event) => setPeriodId(event.target.value)} className="h-10 rounded-md border bg-background px-3">
          <option value="">Select period</option>
          {periods.map((period) => <option key={period.id} value={period.id}>{period.name}</option>)}
        </select>
      </label>
      <label className="grid gap-1 text-sm">Owner
        <select aria-label="Owner" value={owner} onChange={(event) => setOwner(event.target.value)} className="h-10 rounded-md border bg-background px-3">
          <option value="">No owner</option>
          {(members.data ?? []).map((member) => <option key={member.id} value={`member:${member.id}`}>{member.name}</option>)}
          {(agents.data ?? []).map((agent) => <option key={agent.id} value={`agent:${agent.id}`}>{agent.name}</option>)}
        </select>
      </label>
      <label className="grid gap-1 text-sm">Confidence
        <input aria-label="Confidence" type="number" min={0} max={100} value={confidence} onChange={(event) => setConfidence(Number(event.target.value))} className="h-10 rounded-md border bg-background px-3" />
      </label>
      <label className="grid gap-1 text-sm">Reported health
        <select aria-label="Reported health" value={reportedHealth} onChange={(event) => setReportedHealth(event.target.value as "unset" | "on_track" | "at_risk" | "off_track")} className="h-10 rounded-md border bg-background px-3">
          <option value="unset">Not reported</option>
          <option value="on_track">On track</option>
          <option value="at_risk">At risk</option>
          <option value="off_track">Off track</option>
        </select>
      </label>
      <label className="grid gap-1 text-sm sm:col-span-2">Strategy connection
        <select aria-label="Strategy connection" value={strategyItemId} onChange={(event) => setStrategyItemId(event.target.value)} className="h-10 rounded-md border bg-background px-3">
          <option value="">No Strategy connection</option>
          {strategyItems.filter((item) => item.state === "active").map((item) => <option key={item.id} value={item.id}>{item.title}</option>)}
        </select>
      </label>
      <label className="grid gap-1 text-sm">Projects <span className="text-xs text-muted-foreground">Optional · select multiple</span>
        <select aria-label="Projects" multiple value={projectIds} onChange={(event) => setProjectIds(selectedValues(event.target))} className="min-h-28 rounded-md border bg-background px-3 py-2">
          {(projects.data ?? []).map((project) => <option key={project.id} value={project.id}>{project.title}</option>)}
        </select>
      </label>
      <label className="grid gap-1 text-sm">Issues <span className="text-xs text-muted-foreground">Optional · select multiple</span>
        <select aria-label="Issues" multiple value={issueIds} onChange={(event) => setIssueIds(selectedValues(event.target))} className="min-h-28 rounded-md border bg-background px-3 py-2">
          {(issues.data ?? []).map((issue) => <option key={issue.id} value={issue.id}>{issue.identifier} · {issue.title}</option>)}
        </select>
      </label>
      {save.isError && <p role="alert" className="text-sm text-destructive sm:col-span-2">The {terminology.rock.toLowerCase()} could not be saved.</p>}
      <div className="flex justify-end gap-2 sm:col-span-2">
        <button type="button" onClick={onCancel} className="h-10 rounded-md border px-4 text-sm font-medium">Cancel</button>
        <button disabled={save.isPending} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Save {terminology.rock}</button>
      </div>
    </form>
  );
}
