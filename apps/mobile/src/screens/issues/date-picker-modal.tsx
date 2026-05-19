import { useEffect, useMemo, useState } from "react";
import {
  Modal,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/primitives";
import { colors, radii, spacing } from "../../theme/tokens";

export function DatePickerModal({
  disabledDateMessage,
  maxDate,
  minDate,
  onChange,
  onClose,
  open,
  title,
  value,
}: {
  disabledDateMessage?: string;
  maxDate?: string | null;
  minDate?: string | null;
  onChange: (value: string | null) => void;
  onClose: () => void;
  open: boolean;
  title?: string;
  value: string | null | undefined;
}) {
  const { t } = useTranslation();
  const normalizedValue = normalizeDateInput(value);
  const normalizedMinDate = normalizeDateInput(minDate);
  const normalizedMaxDate = normalizeDateInput(maxDate);
  const selectedDate = useMemo(() => parseDateInput(normalizedValue ?? ""), [normalizedValue]);
  const [visibleMonth, setVisibleMonth] = useState(() => selectedDate ?? startOfUtcMonth(new Date()));
  const days = useMemo(() => getCalendarDays(visibleMonth), [visibleMonth]);
  const todayValue = formatDateInput(new Date());
  const todayDisabled = isDateDisabled(todayValue, normalizedMinDate, normalizedMaxDate);

  useEffect(() => {
    if (open) setVisibleMonth(selectedDate ?? startOfUtcMonth(new Date()));
  }, [open, selectedDate]);

  function selectDate(date: Date) {
    const dateValue = formatDateInput(date);
    if (isDateDisabled(dateValue, normalizedMinDate, normalizedMaxDate)) return;
    onChange(dateValue);
    onClose();
  }

  function shiftMonth(delta: number) {
    setVisibleMonth((current) => new Date(Date.UTC(
      current.getUTCFullYear(),
      current.getUTCMonth() + delta,
      1,
    )));
  }

  return (
    <Modal animationType="fade" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.datePickerRoot}>
        <Pressable onPress={onClose} style={styles.datePickerBackdrop} />
        <View style={styles.datePickerCard}>
          {title ? <Text style={styles.datePickerModalTitle}>{title}</Text> : null}
          <View style={styles.datePickerHeader}>
            <Button onPress={() => shiftMonth(-1)} variant="ghost">
              {t("issues.prev")}
            </Button>
            <Text style={styles.datePickerTitle}>{formatMonthLabel(visibleMonth)}</Text>
            <Button onPress={() => shiftMonth(1)} variant="ghost">
              {t("issues.next")}
            </Button>
          </View>
          <View style={styles.weekdayRow}>
            {["S", "M", "T", "W", "T", "F", "S"].map((day, index) => (
              <Text key={`${day}-${index}`} style={styles.weekdayText}>{day}</Text>
            ))}
          </View>
          <View style={styles.calendarGrid}>
            {days.map((day) => {
              const dateValue = formatDateInput(day.date);
              const isSelected = normalizedValue === dateValue;
              const isCurrentMonth = day.date.getUTCMonth() === visibleMonth.getUTCMonth();
              const isDisabled = isDateDisabled(dateValue, normalizedMinDate, normalizedMaxDate);
              return (
                <Pressable
                  accessibilityRole="button"
                  disabled={isDisabled}
                  key={dateValue}
                  onPress={() => selectDate(day.date)}
                  style={({ pressed }) => [
                    styles.calendarDay,
                    isSelected && styles.calendarDaySelected,
                    isDisabled && styles.calendarDayDisabled,
                    pressed && !isDisabled && styles.optionPressed,
                  ]}
                >
                  <Text
                    style={[
                      styles.calendarDayText,
                      !isCurrentMonth && styles.calendarDayMuted,
                      isDisabled && styles.calendarDayTextDisabled,
                      isSelected && styles.calendarDayTextSelected,
                    ]}
                  >
                    {day.date.getUTCDate()}
                  </Text>
                </Pressable>
              );
            })}
          </View>
          {disabledDateMessage ? (
            <Text style={styles.datePickerConstraintText}>{disabledDateMessage}</Text>
          ) : null}
          <View style={styles.datePickerActions}>
            <Button
              onPress={() => {
                onChange(null);
                onClose();
              }}
              variant="secondary"
            >
              {t("issues.clear")}
            </Button>
            <Button disabled={todayDisabled} onPress={() => selectDate(new Date())} variant="secondary">
              {t("issues.today")}
            </Button>
            <Button onPress={onClose} variant="ghost">
              {t("common.close")}
            </Button>
          </View>
        </View>
      </View>
    </Modal>
  );
}

export function isValidDateInput(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const date = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
}

export function normalizeDueDateInput(value: string | null | undefined) {
  return normalizeDateInput(value);
}

export function normalizeDateInput(value: string | null | undefined) {
  if (!value) return null;
  const dateInput = value.slice(0, 10);
  return isValidDateInput(dateInput) ? dateInput : null;
}

export function dateInputToRfc3339(value: string) {
  return `${value}T00:00:00Z`;
}

function parseDateInput(value: string) {
  if (!isValidDateInput(value)) return null;
  const [year, month, day] = value.split("-").map(Number);
  return new Date(Date.UTC(year!, month! - 1, day!));
}

function startOfUtcMonth(date: Date) {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1));
}

export function formatDateInput(date: Date) {
  return new Date(Date.UTC(
    date.getUTCFullYear(),
    date.getUTCMonth(),
    date.getUTCDate(),
  )).toISOString().slice(0, 10);
}

export function formatDueDateLabel(value: string | null | undefined) {
  return formatDateLabel(value);
}

export function formatDateLabel(value: string | null | undefined) {
  const normalizedValue = normalizeDateInput(value);
  if (!normalizedValue) return value ?? "";
  const date = parseDateInput(normalizedValue);
  if (!date) return value ?? "";
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}

function isDateDisabled(
  value: string,
  minDate: string | null,
  maxDate: string | null,
) {
  if (minDate && value < minDate) return true;
  if (maxDate && value > maxDate) return true;
  return false;
}

function formatMonthLabel(date: Date) {
  return date.toLocaleDateString(undefined, {
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });
}

function getCalendarDays(monthDate: Date) {
  const monthStart = startOfUtcMonth(monthDate);
  const firstGridDay = new Date(Date.UTC(
    monthStart.getUTCFullYear(),
    monthStart.getUTCMonth(),
    1 - monthStart.getUTCDay(),
  ));

  return Array.from({ length: 42 }, (_, index) => ({
    date: new Date(Date.UTC(
      firstGridDay.getUTCFullYear(),
      firstGridDay.getUTCMonth(),
      firstGridDay.getUTCDate() + index,
    )),
  }));
}

const styles = StyleSheet.create({
  datePickerRoot: {
    alignItems: "center",
    flex: 1,
    justifyContent: "center",
    padding: spacing.lg,
  },
  datePickerBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(0, 0, 0, 0.44)",
  },
  datePickerCard: {
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.md,
    maxWidth: 360,
    padding: spacing.md,
    width: "100%",
  },
  datePickerHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  datePickerModalTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
    textAlign: "center",
  },
  datePickerTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
  },
  weekdayRow: {
    flexDirection: "row",
  },
  weekdayText: {
    color: colors.mutedForeground,
    flex: 1,
    fontSize: 12,
    fontWeight: "600",
    textAlign: "center",
  },
  calendarGrid: {
    flexDirection: "row",
    flexWrap: "wrap",
  },
  calendarDay: {
    alignItems: "center",
    aspectRatio: 1,
    borderRadius: radii.md,
    justifyContent: "center",
    width: `${100 / 7}%`,
  },
  calendarDaySelected: {
    backgroundColor: colors.primary,
  },
  calendarDayDisabled: {
    opacity: 0.36,
  },
  calendarDayText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  calendarDayMuted: {
    color: colors.mutedForeground,
  },
  calendarDayTextSelected: {
    color: colors.primaryForeground,
  },
  calendarDayTextDisabled: {
    color: colors.mutedForeground,
  },
  datePickerConstraintText: {
    color: colors.mutedForeground,
    fontSize: 12,
    lineHeight: 16,
  },
  datePickerActions: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
    justifyContent: "flex-end",
  },
  optionPressed: {
    opacity: 0.8,
  },
});
