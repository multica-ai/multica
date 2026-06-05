import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import {
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useTranslation } from "react-i18next";
import { Check, ChevronRight, Minus, Plus } from "lucide-react-native";
import { Button } from "../../components/ui/primitives";
import { colors, radii, spacing } from "../../theme/tokens";
import {
  getLocalTimezone,
  toCronExpression,
  type TriggerFormConfig,
  type TriggerFrequency,
} from "./autopilot-mobile-utils";
import { CronSyntaxHelpModal } from "./cron-syntax-help-modal";

type PickerItem<T extends string> = {
  label: string;
  value: T;
};

const FREQUENCY_KEYS: TriggerFrequency[] = [
  "hourly",
  "daily",
  "weekdays",
  "weekly",
  "custom",
];

const TIMEZONE_OPTIONS = [
  "UTC",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Sao_Paulo",
  "Europe/London",
  "Europe/Paris",
  "Europe/Berlin",
  "Europe/Moscow",
  "Asia/Dubai",
  "Asia/Kolkata",
  "Asia/Singapore",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Australia/Sydney",
  "Pacific/Auckland",
];

const DAY_KEYS = [
  "sunday",
  "monday",
  "tuesday",
  "wednesday",
  "thursday",
  "friday",
  "saturday",
] as const;

const STEPPER_LONG_PRESS_DELAY_MS = 350;
const STEPPER_REPEAT_INTERVAL_MS = 90;

export function AutopilotScheduleFields({
  config,
  cronInputRef,
  onChange,
  onCronFocus,
}: {
  config: TriggerFormConfig;
  cronInputRef?: RefObject<TextInput | null>;
  onChange: (config: TriggerFormConfig) => void;
  onCronFocus?: () => void;
}) {
  const { t } = useTranslation();
  const [frequencyOpen, setFrequencyOpen] = useState(false);
  const [dayOpen, setDayOpen] = useState(false);
  const [timezoneOpen, setTimezoneOpen] = useState(false);
  const [cronHelpOpen, setCronHelpOpen] = useState(false);
  const timezones = useMemo(() => {
    const local = getLocalTimezone();
    return TIMEZONE_OPTIONS.includes(local) ? TIMEZONE_OPTIONS : [local, ...TIMEZONE_OPTIONS];
  }, []);
  const frequencyItems = FREQUENCY_KEYS.map((frequency) => ({
    label: t(`autopilots.frequency_${frequency}`),
    value: frequency,
  }));
  const selectedDay = config.daysOfWeek[0] ?? 1;

  const updateFrequency = (frequency: TriggerFrequency) => {
    onChange({
      ...config,
      frequency,
      daysOfWeek: frequency === "weekly" ? [selectedDay] : config.daysOfWeek,
      time: frequency === "hourly"
        ? `00:${getMinute(config.time).toString().padStart(2, "0")}`
        : config.time,
    });
  };

  return (
    <View style={styles.root}>
      <PickerButton
        label={t("autopilots.frequency")}
        onPress={() => setFrequencyOpen(true)}
        value={t(`autopilots.frequency_${config.frequency}`)}
      />

      {config.frequency === "custom" ? (
        <View style={styles.fieldWrap}>
          <Text style={styles.fieldLabel}>{t("autopilots.cron")}</Text>
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            onChangeText={(cronExpression) => onChange({ ...config, cronExpression })}
            onFocus={onCronFocus}
            placeholder="0 9 * * 1-5"
            placeholderTextColor={colors.mutedForeground}
            ref={cronInputRef}
            style={[styles.field, styles.monoField]}
            value={config.cronExpression}
          />
          <View style={styles.helpRow}>
            <Text style={styles.helpText}>{t("autopilots.cron_hint")}</Text>
            <Pressable
              accessibilityLabel={t("autopilots.cron_help_open")}
              accessibilityRole="button"
              onPress={() => setCronHelpOpen(true)}
              style={({ pressed }) => [styles.helpIconButton, pressed && styles.pressed]}
            >
              <Text style={styles.helpIconText}>?</Text>
            </Pressable>
          </View>
          <CronSyntaxHelpModal
            onClose={() => setCronHelpOpen(false)}
            visible={cronHelpOpen}
          />
        </View>
      ) : config.frequency === "hourly" ? (
        <StepperRow
          label={t("autopilots.minute")}
          max={59}
          min={0}
          onChange={(minute) =>
            onChange({ ...config, time: `00:${minute.toString().padStart(2, "0")}` })
          }
          value={getMinute(config.time)}
          valueText={`:${getMinute(config.time).toString().padStart(2, "0")}`}
        />
      ) : (
        <>
          {config.frequency === "weekly" ? (
            <PickerButton
              label={t("autopilots.day")}
              onPress={() => setDayOpen(true)}
              value={t(`autopilots.day_${DAY_KEYS[selectedDay] ?? "monday"}`)}
            />
          ) : null}
          <TimeStepper
            onChange={(time) => onChange({ ...config, time })}
            value={config.time}
          />
          <PickerButton
            label={t("autopilots.timezone")}
            onPress={() => setTimezoneOpen(true)}
            value={formatTimezoneLabel(config.timezone)}
          />
        </>
      )}

      <Text style={styles.helpText}>
        {t("autopilots.cron_preview")}: {toCronExpression(config) || "--"}
      </Text>

      <SelectionModal
        items={frequencyItems}
        onClose={() => setFrequencyOpen(false)}
        onSelect={(item) => {
          updateFrequency(item.value);
          setFrequencyOpen(false);
        }}
        open={frequencyOpen}
        selectedValue={config.frequency}
        title={t("autopilots.frequency")}
      />
      <SelectionModal
        items={DAY_KEYS.map((dayKey, index) => ({
          label: t(`autopilots.day_${dayKey}`),
          value: String(index),
        }))}
        onClose={() => setDayOpen(false)}
        onSelect={(item) => {
          onChange({ ...config, daysOfWeek: [parseInt(item.value, 10)] });
          setDayOpen(false);
        }}
        open={dayOpen}
        selectedValue={String(selectedDay)}
        title={t("autopilots.day")}
      />
      <SelectionModal
        items={timezones.map((timezone) => ({
          label: formatTimezoneLabel(timezone),
          value: timezone,
        }))}
        onClose={() => setTimezoneOpen(false)}
        onSelect={(item) => {
          onChange({ ...config, timezone: item.value });
          setTimezoneOpen(false);
        }}
        open={timezoneOpen}
        selectedValue={config.timezone}
        title={t("autopilots.timezone")}
      />
    </View>
  );
}

function TimeStepper({
  onChange,
  value,
}: {
  onChange: (value: string) => void;
  value: string;
}) {
  const { t } = useTranslation();
  const hour = getHour(value);
  const minute = getMinute(value);
  return (
    <View style={styles.timeGrid}>
      <StepperRow
        label={t("autopilots.hour")}
        max={23}
        min={0}
        onChange={(nextHour) => onChange(formatTime(nextHour, minute))}
        value={hour}
        valueText={String(hour).padStart(2, "0")}
      />
      <StepperRow
        label={t("autopilots.minute")}
        max={59}
        min={0}
        onChange={(nextMinute) => onChange(formatTime(hour, nextMinute))}
        value={minute}
        valueText={String(minute).padStart(2, "0")}
      />
    </View>
  );
}

function StepperRow({
  label,
  max,
  min,
  onChange,
  value,
  valueText,
}: {
  label: string;
  max: number;
  min: number;
  onChange: (value: number) => void;
  value: number;
  valueText: string;
}) {
  const valueRef = useRef(value);
  const repeatTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const repeatIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const hasRepeatedRef = useRef(false);

  useEffect(() => {
    valueRef.current = value;
  }, [value]);

  const clearRepeat = useCallback(() => {
    if (repeatTimeoutRef.current) {
      clearTimeout(repeatTimeoutRef.current);
      repeatTimeoutRef.current = null;
    }
    if (repeatIntervalRef.current) {
      clearInterval(repeatIntervalRef.current);
      repeatIntervalRef.current = null;
    }
  }, []);

  useEffect(() => clearRepeat, [clearRepeat]);

  const step = useCallback((direction: -1 | 1) => {
    const next = wrapNumber(valueRef.current + direction, min, max);
    valueRef.current = next;
    onChange(next);
  }, [max, min, onChange]);

  const beginPress = useCallback((direction: -1 | 1) => {
    clearRepeat();
    hasRepeatedRef.current = false;
    repeatTimeoutRef.current = setTimeout(() => {
      hasRepeatedRef.current = true;
      step(direction);
      repeatIntervalRef.current = setInterval(() => {
        step(direction);
      }, STEPPER_REPEAT_INTERVAL_MS);
    }, STEPPER_LONG_PRESS_DELAY_MS);
  }, [clearRepeat, step]);

  const endPress = useCallback((direction: -1 | 1) => {
    const repeated = hasRepeatedRef.current;
    clearRepeat();
    hasRepeatedRef.current = false;
    if (!repeated) {
      step(direction);
    }
  }, [clearRepeat, step]);

  return (
    <View style={styles.stepperRow}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <View style={styles.stepperControls}>
        <Pressable
          accessibilityLabel={`${label} -1`}
          accessibilityRole="button"
          onPressIn={() => beginPress(-1)}
          onPressOut={() => endPress(-1)}
          style={({ pressed }) => [
            styles.stepperButton,
            pressed && styles.pressed,
          ]}
        >
          <Minus color={colors.foreground} size={16} />
        </Pressable>
        <Text style={styles.stepperValue}>{valueText}</Text>
        <Pressable
          accessibilityLabel={`${label} +1`}
          accessibilityRole="button"
          onPressIn={() => beginPress(1)}
          onPressOut={() => endPress(1)}
          style={({ pressed }) => [
            styles.stepperButton,
            pressed && styles.pressed,
          ]}
        >
          <Plus color={colors.foreground} size={16} />
        </Pressable>
      </View>
    </View>
  );
}

function PickerButton({
  label,
  onPress,
  value,
}: {
  label: string;
  onPress: () => void;
  value: string;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.pickerButton, pressed && styles.pressed]}
    >
      <View style={styles.pickerText}>
        <Text style={styles.fieldLabel}>{label}</Text>
        <Text numberOfLines={1} style={styles.pickerValue}>
          {value}
        </Text>
      </View>
      <ChevronRight color={colors.mutedForeground} size={18} />
    </Pressable>
  );
}

function SelectionModal<T extends string>({
  items,
  onClose,
  onSelect,
  open,
  selectedValue,
  title,
}: {
  items: Array<PickerItem<T>>;
  onClose: () => void;
  onSelect: (item: PickerItem<T>) => void;
  open: boolean;
  selectedValue: T;
  title: string;
}) {
  const { t } = useTranslation();
  return (
    <Modal animationType="slide" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.modalBackdrop}>
        <View style={styles.modalSheet}>
          <Text style={styles.modalTitle}>{title}</Text>
          <ScrollView contentContainerStyle={styles.modalList}>
            {items.map((item) => {
              const selected = item.value === selectedValue;
              return (
                <Pressable
                  accessibilityRole="button"
                  key={item.value}
                  onPress={() => onSelect(item)}
                  style={({ pressed }) => [
                    styles.optionRow,
                    selected && styles.optionRowSelected,
                    pressed && styles.pressed,
                  ]}
                >
                  <Text numberOfLines={1} style={styles.optionLabel}>
                    {item.label}
                  </Text>
                  {selected ? <Check color={colors.foreground} size={16} /> : null}
                </Pressable>
              );
            })}
          </ScrollView>
          <Button onPress={onClose} variant="secondary">
            {t("common.close")}
          </Button>
        </View>
      </View>
    </Modal>
  );
}

function getHour(value: string): number {
  const hour = parseInt(value.split(":")[0] ?? "9", 10);
  return Number.isFinite(hour) ? Math.min(Math.max(hour, 0), 23) : 9;
}

function getMinute(value: string): number {
  const minute = parseInt(value.split(":")[1] ?? "0", 10);
  return Number.isFinite(minute) ? Math.min(Math.max(minute, 0), 59) : 0;
}

function formatTime(hour: number, minute: number): string {
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

function wrapNumber(value: number, min: number, max: number): number {
  if (value > max) return min;
  if (value < min) return max;
  return value;
}

function formatTimezoneLabel(timezone: string): string {
  if (timezone === "UTC") return "UTC";
  const city = timezone.split("/").pop()?.replace(/_/g, " ") ?? timezone;
  return `${city} (${timezone})`;
}

const styles = StyleSheet.create({
  root: {
    gap: spacing.md,
  },
  pickerButton: {
    alignItems: "center",
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 52,
    paddingHorizontal: spacing.md,
  },
  pickerText: {
    flex: 1,
    gap: 2,
    minWidth: 0,
  },
  pickerValue: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
  },
  fieldWrap: {
    gap: spacing.xs,
  },
  fieldLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  field: {
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 15,
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  monoField: {
    fontFamily: "Menlo",
  },
  helpText: {
    color: colors.mutedForeground,
    fontSize: 12,
    lineHeight: 17,
  },
  helpRow: {
    alignItems: "center",
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.xs,
  },
  helpIconButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderColor: colors.border,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    height: 20,
    justifyContent: "center",
    width: 20,
  },
  helpIconText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "700",
    lineHeight: 15,
  },
  timeGrid: {
    flexDirection: "row",
    gap: spacing.sm,
  },
  stepperRow: {
    flex: 1,
    gap: spacing.xs,
    minWidth: 0,
  },
  stepperControls: {
    alignItems: "center",
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    minHeight: 44,
    overflow: "hidden",
  },
  stepperButton: {
    alignItems: "center",
    height: 44,
    justifyContent: "center",
    width: 42,
  },
  stepperValue: {
    color: colors.foreground,
    flex: 1,
    fontSize: 16,
    fontVariant: ["tabular-nums"],
    fontWeight: "600",
    textAlign: "center",
  },
  pressed: {
    opacity: 0.72,
  },
  modalBackdrop: {
    backgroundColor: "rgba(24,24,27,0.35)",
    flex: 1,
    justifyContent: "flex-end",
  },
  modalSheet: {
    backgroundColor: colors.background,
    borderTopLeftRadius: radii.md,
    borderTopRightRadius: radii.md,
    gap: spacing.md,
    maxHeight: "86%",
    padding: spacing.lg,
  },
  modalTitle: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "600",
  },
  modalList: {
    gap: spacing.sm,
  },
  optionRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "space-between",
    minHeight: 46,
    paddingHorizontal: spacing.md,
  },
  optionRowSelected: {
    borderColor: colors.foreground,
  },
  optionLabel: {
    color: colors.foreground,
    flex: 1,
    fontSize: 14,
    fontWeight: "600",
  },
});
