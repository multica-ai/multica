import { StyleSheet, TextInput, View } from "react-native";
import type { ProjectPriority, ProjectStatus } from "@multica/core/types";
import { Avatar } from "../../components/Avatar";
import { DateTimeInput } from "../../components/DateTimeInput";
import { ChoiceList, Field, FormBody, SheetHeader, type Choice } from "../../components/Form";
import { Group, GroupRow } from "../../components/Group";
import { Ring } from "../../components/Progress";
import { Screen } from "../../components/Screen";
import { PriorityIcon } from "../../components/StatusIcon";
import { formatDay, parseDay, toDay } from "../../model/dates";
import { PROJECT_STATUSES, projectStatusColor } from "../../model/projects";
import type { RosterEntry } from "../../model/roster";
import { useStrings } from "../../strings";
import { radius, type as typeRamp, useTheme } from "../../theme";

export interface ProjectDraft {
  icon: string;
  title: string;
  description: string;
  status: ProjectStatus;
  priority: ProjectPriority;
  /** A roster value, `member:<id>` or `agent:<id>`. */
  lead: string | null;
  start: string | null;
  due: string | null;
}

export type ProjectEditorPage = "form" | "status" | "priority" | "lead" | "start" | "due";

const PRIORITIES: readonly ProjectPriority[] = ["urgent", "high", "medium", "low", "none"];

interface ProjectEditorViewProps {
  mode: "create" | "edit";
  draft: ProjectDraft;
  onChange: (patch: Partial<ProjectDraft>) => void;
  page: ProjectEditorPage;
  onPage: (page: ProjectEditorPage) => void;
  leads: readonly RosterEntry[];
  busy: boolean;
  onCancel: () => void;
  onSave: () => void;
}

export function ProjectEditorView({ mode, draft, onChange, page, onPage, leads, busy, onCancel, onSave }: ProjectEditorViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  const back = { label: strings.common.back, onPress: () => onPage("form"), back: true };
  const leadEntry = leads.find((entry) => entry.value === draft.lead) ?? null;

  if (page === "status" || page === "priority" || page === "lead") {
    const choices: Choice<string>[] =
      page === "status"
        ? PROJECT_STATUSES.map((value) => ({ value, label: strings.project.status[value] ?? value, leading: <Ring progress={value === "completed" ? 1 : 0.5} color={projectStatusColor(palette, value)} size={16} /> }))
        : page === "priority"
          ? PRIORITIES.map((value) => ({ value, label: strings.priority[value], leading: <PriorityIcon priority={value} size={16} /> }))
          : [{ value: "none", label: strings.project.noLead }, ...leads.map((entry) => ({ value: entry.value, label: entry.actor.name, detail: entry.detail, leading: <Avatar actor={entry.actor} size={24} /> }))];
    const selected = page === "status" ? draft.status : page === "priority" ? draft.priority : (draft.lead ?? "none");
    return (
      <Screen>
        <SheetHeader title={page === "status" ? strings.project.statusTitle : page === "priority" ? strings.issue.priorityTitle : strings.project.lead} leading={back} />
        <ChoiceList
          choices={choices}
          selected={[selected]}
          searchPlaceholder={strings.form.searchPlaceholder}
          onToggle={(value) => {
            if (page === "status") onChange({ status: value as ProjectStatus });
            else if (page === "priority") onChange({ priority: value as ProjectPriority });
            else onChange({ lead: value === "none" ? null : value });
            onPage("form");
          }}
        />
      </Screen>
    );
  }

  if (page === "start" || page === "due") {
    const current = page === "start" ? draft.start : draft.due;
    return (
      <Screen>
        <SheetHeader
          title={page === "start" ? strings.project.start : strings.project.due}
          leading={back}
          trailing={
            current
              ? {
                  label: strings.form.clear,
                  onPress: () => {
                    onChange(page === "start" ? { start: null } : { due: null });
                    onPage("form");
                  },
                }
              : undefined
          }
        />
        <View style={styles.calendar}>
          <DateTimeInput
            mode="date"
            value={(current && parseDay(current)) || new Date()}
            onChange={(date) => {
              onChange(page === "start" ? { start: toDay(date) } : { due: toDay(date) });
              onPage("form");
            }}
          />
        </View>
      </Screen>
    );
  }

  return (
    <Screen>
      <SheetHeader
        title={mode === "create" ? strings.project.new : strings.project.edit}
        leading={{ label: strings.common.cancel, onPress: onCancel }}
        trailing={{ label: mode === "create" ? strings.form.create : strings.form.save, onPress: onSave, disabled: draft.title.trim() === "", busy }}
      />
      <FormBody>
        <View style={styles.titleRow}>
          <TextInput
            value={draft.icon}
            onChangeText={(icon) => onChange({ icon })}
            placeholder="📁"
            placeholderTextColor={palette.label3}
            accessibilityLabel={strings.project.icon}
            maxLength={8}
            style={[typeRamp.body, styles.emoji, { backgroundColor: palette.grouped, color: palette.label }]}
          />
          <View style={styles.flex}>
            <Field value={draft.title} onChangeText={(title) => onChange({ title })} placeholder={strings.project.namePlaceholder} autoFocus={mode === "create"} large />
          </View>
        </View>
        <Field value={draft.description} onChangeText={(description) => onChange({ description })} placeholder={strings.project.descriptionPlaceholder} multiline />
        <Group>
          <GroupRow
            leading={<Ring progress={draft.status === "completed" ? 1 : 0.5} color={projectStatusColor(palette, draft.status)} size={18} />}
            label={strings.project.statusTitle}
            value={strings.project.status[draft.status]}
            chevron
            onPress={() => onPage("status")}
          />
          <GroupRow leading={<PriorityIcon priority={draft.priority} size={18} />} label={strings.issue.priorityTitle} value={strings.priority[draft.priority]} chevron onPress={() => onPage("priority")} />
          <GroupRow
            leading={leadEntry ? <Avatar actor={leadEntry.actor} size={20} /> : undefined}
            icon={leadEntry ? undefined : "CircleUser"}
            label={strings.project.lead}
            value={leadEntry?.actor.name ?? strings.project.noLead}
            chevron
            onPress={() => onPage("lead")}
            last
          />
        </Group>
        <Group>
          <GroupRow icon="Calendar" label={strings.project.start} value={draft.start ? formatDay(draft.start, strings) : strings.form.none} chevron onPress={() => onPage("start")} />
          <GroupRow icon="CalendarClock" label={strings.project.due} value={draft.due ? formatDay(draft.due, strings) : strings.form.none} chevron onPress={() => onPage("due")} last />
        </Group>
      </FormBody>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  titleRow: { flexDirection: "row", gap: 10, alignItems: "stretch" },
  emoji: { width: 56, borderRadius: radius.group, textAlign: "center", fontSize: 26, lineHeight: 32 },
  calendar: { paddingHorizontal: 12 },
});
