import { useEffect, useMemo, useState } from "react";
import { Alert } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { childIssuesOptions, issueDetailOptions } from "@multica/core/issues/queries";
import { issueLabelsOptions, labelListOptions, useAttachLabel, useDetachLabel } from "@multica/core/labels";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Issue, SearchIssueResult, UpdateIssueRequest } from "@multica/core/types";
import { Failure, Loading } from "../../components/States";
import { parseDay, toDay } from "../../model/dates";
import { parseAssigneeValue, rosterValue, useAssignees } from "../../model/roster";
import { useWorkspace } from "../../model/workspace-context";
import type { IssueFieldKey, RootStackParamList } from "../../navigation/types";
import { useStrings, type Strings } from "../../strings";
import { IssueFieldView, type FieldChoice } from "./IssueFieldView";

const NONE = "none";
const SEARCH_DELAY = 250;

export function IssueFieldScreen() {
  const { issueId, field } = useRoute<RouteProp<RootStackParamList, "IssueField">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const detail = useQuery(issueDetailOptions(workspace.id, issueId));
  if (!detail.data) {
    return detail.isError ? <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={() => void detail.refetch()} /> : <Loading />;
  }
  return <IssueField issue={detail.data} field={field} />;
}

function titleOf(field: IssueFieldKey, strings: Strings): string {
  return {
    assignee: strings.issue.assignee,
    project: strings.issue.project,
    labels: strings.issue.labels,
    parent: strings.issue.parent,
    start: strings.issue.start,
    due: strings.issue.due,
  }[field];
}

function IssueField({ issue, field }: { issue: Issue; field: IssueFieldKey }) {
  const navigation = useNavigation();
  const strings = useStrings();
  const updateIssue = useUpdateIssue();
  const close = () => navigation.goBack();

  const save = (fields: Omit<UpdateIssueRequest, "id">) => {
    updateIssue.mutate({ id: issue.id, ...fields }, { onError: () => Alert.alert(strings.issue.updateFailed) });
    close();
  };

  const title = titleOf(field, strings);
  const cancel = { label: strings.common.cancel, onPress: close };
  switch (field) {
    case "assignee":
      return <AssigneeField issue={issue} title={title} cancel={cancel} onSave={save} />;
    case "project":
      return <ProjectField issue={issue} title={title} cancel={cancel} onSave={save} />;
    case "labels":
      return <LabelsField issue={issue} title={title} onDone={close} />;
    case "parent":
      return <ParentField issue={issue} title={title} cancel={cancel} onSave={save} />;
    case "start":
    case "due": {
      const key = field === "start" ? "start_date" : "due_date";
      const current = issue[key];
      return (
        <IssueFieldView
          title={title}
          close={cancel}
          clear={current ? { label: strings.form.clear, onPress: () => save({ [key]: null }) } : undefined}
          date={{ value: (current && parseDay(current)) || new Date(), onChange: (date) => save({ [key]: toDay(date) }) }}
        />
      );
    }
  }
}

interface SingleFieldProps {
  issue: Issue;
  title: string;
  cancel: { label: string; onPress: () => void };
  onSave: (fields: Omit<UpdateIssueRequest, "id">) => void;
}

function AssigneeField({ issue, title, cancel, onSave }: SingleFieldProps) {
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const assignees = useAssignees(workspace.id);
  const choices = useMemo<FieldChoice[]>(
    () => [
      { value: NONE, label: strings.issue.unassigned },
      ...assignees.members.map((entry) => ({ value: entry.value, label: entry.actor.name, actor: entry.actor, detail: entry.detail })),
      ...assignees.agents.map((entry) => ({ value: entry.value, label: entry.actor.name, actor: entry.actor, detail: entry.detail ?? strings.issue.agents })),
      ...assignees.squads.map((entry) => ({ value: entry.value, label: entry.actor.name, actor: entry.actor, detail: entry.detail ?? strings.issue.squads })),
    ],
    [assignees, strings],
  );
  const selected = issue.assignee_type && issue.assignee_id ? rosterValue(issue.assignee_type, issue.assignee_id) : NONE;
  return (
    <IssueFieldView
      title={title}
      close={cancel}
      loading={!assignees.ready}
      list={{
        choices,
        selected: [selected],
        searchPlaceholder: strings.form.searchPlaceholder,
        onToggle: (value) => {
          if (value === selected) return cancel.onPress();
          const target = parseAssigneeValue(value);
          onSave({ assignee_type: target?.type ?? null, assignee_id: target?.id ?? null });
        },
      }}
    />
  );
}

function ProjectField({ issue, title, cancel, onSave }: SingleFieldProps) {
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const projects = useQuery(projectListOptions(workspace.id));
  const choices = useMemo<FieldChoice[]>(
    () => [
      { value: NONE, label: strings.issue.noProject },
      ...(projects.data ?? []).map((project) => ({ value: project.id, label: project.icon ? `${project.icon} ${project.title}` : project.title })),
    ],
    [projects.data, strings],
  );
  const selected = issue.project_id ?? NONE;
  return (
    <IssueFieldView
      title={title}
      close={cancel}
      loading={projects.isPending}
      list={{
        choices,
        selected: [selected],
        searchPlaceholder: strings.form.searchPlaceholder,
        onToggle: (value) => (value === selected ? cancel.onPress() : onSave({ project_id: value === NONE ? null : value })),
      }}
    />
  );
}

/** Labels are a set: each tap attaches or detaches one, and the sheet stays open until Done. */
function LabelsField({ issue, title, onDone }: { issue: Issue; title: string; onDone: () => void }) {
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const all = useQuery(labelListOptions(workspace.id, "issue"));
  const attached = useQuery(issueLabelsOptions(workspace.id, issue.id));
  const attach = useAttachLabel(issue.id);
  const detach = useDetachLabel(issue.id);
  const selected = useMemo(() => (attached.data ?? issue.labels ?? []).map((label) => label.id), [attached.data, issue.labels]);
  const choices = useMemo<FieldChoice[]>(() => (all.data ?? []).map((label) => ({ value: label.id, label: label.name, color: label.color })), [all.data]);
  return (
    <IssueFieldView
      title={title}
      close={{ label: strings.issue.done, onPress: onDone }}
      loading={all.isPending}
      list={{
        choices,
        selected,
        searchPlaceholder: strings.form.searchPlaceholder,
        emptyLabel: strings.form.none,
        onToggle: (value) => {
          const onError = () => Alert.alert(strings.issue.updateFailed);
          if (selected.includes(value)) detach.mutate(value, { onError });
          else attach.mutate(value, { onError });
        },
      }}
    />
  );
}

/** Searches the workspace on the server; the issue itself and its own sub-issues can't be its parent. */
function ParentField({ issue, title, cancel, onSave }: SingleFieldProps) {
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchIssueResult[]>([]);
  const children = useQuery(childIssuesOptions(workspace.id, issue.id));
  const parent = useQuery({ ...issueDetailOptions(workspace.id, issue.parent_issue_id ?? ""), enabled: Boolean(issue.parent_issue_id) });

  useEffect(() => {
    const q = query.trim();
    if (!q) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      api
        .searchIssues({ q, limit: 20, include_closed: true, signal: controller.signal })
        .then((response) => setResults(response.issues))
        .catch(() => undefined);
    }, SEARCH_DELAY);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [query]);

  const choices = useMemo<FieldChoice[]>(() => {
    const excluded = new Set([issue.id, ...(children.data ?? []).map((child) => child.id)]);
    const found = query.trim() ? results : [];
    const rows: FieldChoice[] = [];
    if (issue.parent_issue_id) {
      rows.push({ value: NONE, label: strings.issue.menu.removeParent });
      if (parent.data) rows.push({ value: parent.data.id, label: parent.data.title, detail: parent.data.identifier });
    }
    for (const result of found) {
      if (excluded.has(result.id) || result.id === issue.parent_issue_id) continue;
      rows.push({ value: result.id, label: result.title, detail: result.identifier });
    }
    return rows;
  }, [issue, children.data, parent.data, results, query, strings]);

  return (
    <IssueFieldView
      title={title}
      close={cancel}
      list={{
        choices,
        selected: issue.parent_issue_id ? [issue.parent_issue_id] : [],
        searchPlaceholder: strings.issue.searchIssues,
        search: { query, onQueryChange: setQuery },
        onToggle: (value) => {
          if (value === issue.parent_issue_id) return cancel.onPress();
          onSave({ parent_issue_id: value === NONE ? null : value });
        },
      }}
    />
  );
}
