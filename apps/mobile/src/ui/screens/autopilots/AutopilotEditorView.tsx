import type { ScheduleConfig } from "@multica/core/autopilots/schedule";
import type { AutopilotExecutionMode } from "@multica/core/types";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { ChoiceList, Field, FormBody, SheetHeader, type Choice } from "../../components/Form";
import { Group, GroupNote, GroupRow } from "../../components/Group";
import { Screen } from "../../components/Screen";
import { Txt } from "../../components/Text";
import { Icon } from "../../icons/Icon";
import { describeSchedule } from "../../model/schedule";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";
import { ScheduleForm, type NextRunsModel } from "./ScheduleForm";

export interface AutopilotDraft {
  title: string;
  description: string;
  /** `agent:<id>` or `squad:<id>`. */
  assignee: string | null;
  mode: AutopilotExecutionMode;
  /** Project id. */
  project: string | null;
  /** The first schedule trigger; only set while creating. */
  schedule: ScheduleConfig | null;
}

export type AutopilotEditorPage = "form" | "assignee" | "mode" | "project" | "schedule";

export interface AssigneeOption {
  value: string;
  actor: AvatarActor;
  detail: string;
}

export interface ProjectOption {
  id: string;
  title: string;
  icon: string | null;
}

const MODES: readonly AutopilotExecutionMode[] = ["create_issue", "run_only"];

interface AutopilotEditorViewProps {
  mode: "create" | "edit";
  draft: AutopilotDraft;
  onChange: (patch: Partial<AutopilotDraft>) => void;
  page: AutopilotEditorPage;
  onPage: (page: AutopilotEditorPage) => void;
  assignees: readonly AssigneeOption[];
  projects: readonly ProjectOption[];
  nextRuns: NextRunsModel;
  /** Starts a schedule when the creator first opens the schedule page. */
  onAddSchedule: () => void;
  busy: boolean;
  onCancel: () => void;
  onSave: () => void;
}

export function AutopilotEditorView({ mode, draft, onChange, page, onPage, assignees, projects, nextRuns, onAddSchedule, busy, onCancel, onSave }: AutopilotEditorViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  const back = { label: strings.common.back, onPress: () => onPage("form"), back: true };
  const assignee = assignees.find((option) => option.value === draft.assignee) ?? null;
  const project = projects.find((option) => option.id === draft.project) ?? null;
  const projectIcon = (icon: string | null) => (icon ? <Txt variant="body">{icon}</Txt> : <Icon name="Folder" size={18} color={palette.label2} />);

  if (page === "assignee" || page === "mode" || page === "project") {
    const choices: Choice<string>[] =
      page === "assignee"
        ? assignees.map((option) => ({ value: option.value, label: option.actor.name, detail: option.detail, leading: <Avatar actor={option.actor} size={24} /> }))
        : page === "mode"
          ? MODES.map((value) => ({ value, label: strings.autopilot.modes[value] ?? value }))
          : [{ value: "none", label: strings.autopilot.noProject }, ...projects.map((option) => ({ value: option.id, label: option.title, leading: projectIcon(option.icon) }))];
    const selected = page === "assignee" ? (draft.assignee ?? "") : page === "mode" ? draft.mode : (draft.project ?? "none");
    return (
      <Screen>
        <SheetHeader title={page === "assignee" ? strings.autopilot.pickAssignee : page === "mode" ? strings.autopilot.mode : strings.autopilot.project} leading={back} />
        <ChoiceList
          choices={choices}
          selected={[selected]}
          searchPlaceholder={page === "mode" ? undefined : strings.form.searchPlaceholder}
          onToggle={(value) => {
            if (page === "assignee") onChange({ assignee: value });
            else if (page === "mode") onChange({ mode: value as AutopilotExecutionMode });
            else onChange({ project: value === "none" ? null : value });
            onPage("form");
          }}
        />
      </Screen>
    );
  }

  if (page === "schedule" && draft.schedule) {
    return (
      <Screen>
        <SheetHeader
          title={strings.schedule.title}
          leading={back}
          trailing={{
            label: strings.form.clear,
            onPress: () => {
              onChange({ schedule: null });
              onPage("form");
            },
          }}
        />
        <FormBody>
          <ScheduleForm config={draft.schedule} onChange={(schedule) => onChange({ schedule })} nextRuns={nextRuns} />
        </FormBody>
      </Screen>
    );
  }

  return (
    <Screen>
      <SheetHeader
        title={mode === "create" ? strings.autopilot.new : strings.autopilot.edit}
        leading={{ label: strings.common.cancel, onPress: onCancel }}
        trailing={{
          label: mode === "create" ? strings.form.create : strings.form.save,
          onPress: onSave,
          disabled: draft.title.trim() === "" || !draft.assignee || (draft.schedule !== null && nextRuns.state === "error"),
          busy,
        }}
      />
      <FormBody>
        <Field value={draft.title} onChangeText={(title) => onChange({ title })} placeholder={strings.autopilot.titlePlaceholder} autoFocus={mode === "create"} large />
        <Field value={draft.description} onChangeText={(description) => onChange({ description })} placeholder={strings.autopilot.promptPlaceholder} multiline />
        <Group>
          <GroupRow
            leading={assignee ? <Avatar actor={assignee.actor} size={20} /> : undefined}
            icon={assignee ? undefined : "Bot"}
            label={strings.autopilot.assignee}
            value={assignee?.actor.name ?? strings.autopilot.pickAssignee}
            chevron
            onPress={() => onPage("assignee")}
          />
          <GroupRow icon="Play" label={strings.autopilot.mode} value={strings.autopilot.modes[draft.mode]} chevron onPress={() => onPage("mode")} />
          <GroupRow leading={project ? projectIcon(project.icon) : undefined} icon={project ? undefined : "Folder"} label={strings.autopilot.project} value={project?.title ?? strings.form.none} chevron onPress={() => onPage("project")} last />
        </Group>
        {mode === "create" ? (
          <>
            <Group>
              <GroupRow
                icon="CalendarClock"
                label={strings.schedule.title}
                value={draft.schedule ? describeSchedule(draft.schedule, strings) : strings.form.none}
                chevron
                onPress={() => {
                  if (!draft.schedule) onAddSchedule();
                  onPage("schedule");
                }}
                last
              />
            </Group>
            {draft.schedule ? null : <GroupNote>{strings.autopilot.noTriggers}</GroupNote>}
          </>
        ) : null}
      </FormBody>
    </Screen>
  );
}
