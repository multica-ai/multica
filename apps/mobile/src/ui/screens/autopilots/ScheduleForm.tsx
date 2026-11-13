import { Pressable, StyleSheet, View } from "react-native";
import { pad2, timeParts, toggleDay, type ScheduleConfig } from "@multica/core/autopilots/schedule";
import { DateTimeInput } from "../../components/DateTimeInput";
import { Group, GroupNote, GroupRow } from "../../components/Group";
import { Txt } from "../../components/Text";
import { Icon } from "../../icons/Icon";
import { SCHEDULE_REPEATS, scheduleRepeat, withRepeat, type ScheduleRepeat } from "../../model/schedule";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface NextRunsModel {
  state: "loading" | "error" | "ready";
  /** Already formatted; empty means the schedule never fires. */
  items: readonly string[];
}

function Stepper({ value, min, max, onChange }: { value: number; min: number; max: number; onChange: (value: number) => void }) {
  const palette = useTheme();
  const button = (icon: "Minus" | "Plus", next: number, disabled: boolean) => (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={icon === "Plus" ? "+" : "−"}
      disabled={disabled}
      onPress={() => onChange(next)}
      hitSlop={6}
      style={({ pressed }) => [styles.step, { backgroundColor: palette.fill, opacity: disabled ? 0.35 : pressed ? 0.6 : 1 }]}
    >
      <Icon name={icon} size={16} color={palette.label} />
    </Pressable>
  );
  return (
    <View style={styles.stepper}>
      {button("Minus", value - 1, value <= min)}
      {button("Plus", value + 1, value >= max)}
    </View>
  );
}

function DayToggles({ days, onToggle }: { days: readonly number[]; onToggle: (day: number) => void }) {
  const palette = useTheme();
  const strings = useStrings();
  return (
    <View style={styles.days}>
      {strings.schedule.dayNames.map((name, day) => {
        const on = days.includes(day);
        return (
          <Pressable
            key={name}
            accessibilityRole="button"
            accessibilityState={{ selected: on }}
            onPress={() => onToggle(day)}
            style={[styles.day, { backgroundColor: on ? palette.label : palette.fill }]}
          >
            <Txt variant="metaStrong" style={{ color: on ? palette.background : palette.label }}>
              {name.replace(/^周/, "")}
            </Txt>
          </Pressable>
        );
      })}
    </View>
  );
}

const timeDate = (time: string) => {
  const { hour, minute } = timeParts(time);
  const date = new Date();
  date.setHours(hour, minute, 0, 0);
  return date;
};

interface ScheduleFormProps {
  config: ScheduleConfig;
  onChange: (config: ScheduleConfig) => void;
  nextRuns: NextRunsModel;
}

/** Repeat, days and time for a schedule trigger, with the server's next runs underneath. */
export function ScheduleForm({ config, onChange, nextRuns }: ScheduleFormProps) {
  const strings = useStrings();
  const palette = useTheme();
  const repeat = scheduleRepeat(config);
  const labels: Record<ScheduleRepeat, string> = {
    daily: strings.schedule.daily,
    weekdays: strings.schedule.weekdays,
    weekly: strings.schedule.weekly,
    monthly: strings.schedule.monthly,
    hourly: strings.schedule.hourly,
  };

  if (repeat === null) {
    return (
      <>
        <Group>
          <GroupRow icon="CalendarClock" label={strings.schedule.custom} detail={config.raw ?? ""} last />
        </Group>
        <GroupNote>{strings.schedule.customDetail}</GroupNote>
      </>
    );
  }

  return (
    <>
      <GroupNote above>{strings.schedule.repeat}</GroupNote>
      <Group>
        {SCHEDULE_REPEATS.map((option, index) => (
          <GroupRow
            key={option}
            label={labels[option]}
            last={index === SCHEDULE_REPEATS.length - 1}
            right={repeat === option ? <Icon name="Check" size={18} color={palette.accent} /> : undefined}
            onPress={() => onChange(withRepeat(config, option))}
          />
        ))}
      </Group>

      {config.days.kind === "weekly" && repeat === "weekly" ? (
        <>
          <GroupNote above>{strings.schedule.days}</GroupNote>
          <DayToggles days={config.days.daysOfWeek} onToggle={(day) => config.days.kind === "weekly" && onChange({ ...config, days: { kind: "weekly", daysOfWeek: toggleDay(config.days.daysOfWeek, day) } })} />
        </>
      ) : null}

      {config.days.kind === "monthly" ? (
        <Group>
          <GroupRow
            label={strings.schedule.dayOfMonth}
            value={strings.schedule.dayN(config.days.dayOfMonth)}
            right={<Stepper value={config.days.dayOfMonth} min={1} max={31} onChange={(dayOfMonth) => onChange({ ...config, days: { kind: "monthly", dayOfMonth } })} />}
            last
          />
        </Group>
      ) : null}

      {config.time.kind === "every" ? (
        <Group>
          <GroupRow
            label={strings.schedule.interval}
            value={strings.schedule.everyN(config.time.interval)}
            right={<Stepper value={config.time.interval} min={1} max={12} onChange={(interval) => config.time.kind === "every" && onChange({ ...config, time: { ...config.time, interval } })} />}
            last
          />
        </Group>
      ) : (
        <>
          <GroupNote above>{strings.schedule.time}</GroupNote>
          <Group>
            <DateTimeInput
              mode="time"
              value={timeDate(config.time.time)}
              onChange={(date) => onChange({ ...config, time: { kind: "at", time: `${pad2(date.getHours())}:${pad2(date.getMinutes())}` } })}
            />
          </Group>
        </>
      )}

      <Group>
        <GroupRow icon="Clock" label={strings.schedule.timezone} value={config.timezone} last />
      </Group>

      <GroupNote above>{strings.schedule.nextRuns}</GroupNote>
      <Group>
        {nextRuns.state === "loading" ? (
          <GroupRow label="…" last />
        ) : nextRuns.state === "error" ? (
          <GroupRow label={strings.schedule.invalid} destructive last />
        ) : nextRuns.items.length === 0 ? (
          <GroupRow label={strings.schedule.never} last />
        ) : (
          nextRuns.items.map((item, index) => <GroupRow key={`${item}-${index}`} label={item} last={index === nextRuns.items.length - 1} />)
        )}
      </Group>
    </>
  );
}

const styles = StyleSheet.create({
  stepper: { flexDirection: "row", gap: 8 },
  step: { width: 32, height: 32, borderRadius: 16, alignItems: "center", justifyContent: "center" },
  days: { flexDirection: "row", justifyContent: "space-between" },
  day: { width: 40, height: 40, borderRadius: 20, alignItems: "center", justifyContent: "center" },
});
