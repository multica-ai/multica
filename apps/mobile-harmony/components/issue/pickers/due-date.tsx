/**
 * HarmonyOS port of apps/mobile/components/issue/pickers/due-date-picker-body.tsx
 * (+ `issue/[id]/picker/due-date.tsx`).
 *
 * Platform substitution: the iOS body wraps the native UIDatePicker
 * (display="inline"); `@react-native-community/datetimepicker` is not on
 * the RNOH matrix. The replacement is a pure-RN inline calendar — which is
 * what UIDatePicker's inline style renders anyway: quick-preset chips
 * (Today / Tomorrow / Next week) above a month grid with prev/next month
 * chevrons. Done / Clear live in the sheet header exactly like the iOS
 * route's mini header.
 *
 * due_date is a calendar day (date-only "YYYY-MM-DD", no time/timezone —
 * see @multica/core/issues/date and GH #3618): read the stored day into a
 * local-midnight Date, write back the picked local day as a date-only
 * string.
 */
import React, {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { StyleSheet, Text, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@/components/ui/icon";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import {
  toDateOnly,
  dateOnlyToLocalDate,
} from "@multica/core/issues/date";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";

export interface DueDatePickerBodyHandle {
  /** Returns the currently-displayed day as a date-only "YYYY-MM-DD" string. */
  getIso: () => string;
}

const WEEKDAY_LABELS = ["S", "M", "T", "W", "T", "F", "S"] as const;

function startOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1);
}
function addMonths(date: Date, months: number): Date {
  return new Date(date.getFullYear(), date.getMonth() + months, 1);
}
function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}
/** Midnight-local day for an offset from today. */
function todayPlus(days: number): Date {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function toLocalDay(value: string | null): Date {
  return dateOnlyToLocalDate(value) ?? todayPlus(0);
}

interface Preset {
  label: string;
  days: number;
}
const PRESETS: Preset[] = [
  { label: "Today", days: 0 },
  { label: "Tomorrow", days: 1 },
  { label: "Next week", days: 7 },
];

interface Props {
  value: string | null;
}

export const DueDatePickerBody = forwardRef<DueDatePickerBodyHandle, Props>(
  function DueDatePickerBody({ value }, ref) {
    const c = useThemeColors();
    const [draft, setDraft] = useState<Date>(() => toLocalDay(value));
    const [visibleMonth, setVisibleMonth] = useState<Date>(() =>
      startOfMonth(toLocalDay(value)),
    );

    useEffect(() => {
      const next = toLocalDay(value);
      setDraft(next);
      setVisibleMonth(startOfMonth(next));
    }, [value]);

    useImperativeHandle(ref, () => ({
      getIso: () => toDateOnly(draft),
    }));

    // 6-row grid for the visible month (leading blanks pad to the weekday).
    const cells = useMemo(() => {
      const year = visibleMonth.getFullYear();
      const month = visibleMonth.getMonth();
      const firstWeekday = new Date(year, month, 1).getDay();
      const daysInMonth = new Date(year, month + 1, 0).getDate();
      const out: (Date | null)[] = [];
      for (let i = 0; i < firstWeekday; i++) out.push(null);
      for (let day = 1; day <= daysInMonth; day++) {
        out.push(new Date(year, month, day));
      }
      return out;
    }, [visibleMonth]);

    const monthLabel = visibleMonth.toLocaleDateString("en-US", {
      month: "long",
      year: "numeric",
    });

    return (
      <View style={styles.wrap}>
        {/* Quick presets */}
        <View style={styles.presets}>
          {PRESETS.map((preset) => {
            const presetDay = todayPlus(preset.days);
            const active = isSameDay(draft, presetDay);
            return (
              <Pressable
                key={preset.label}
                onPress={() => {
                  setDraft(presetDay);
                  setVisibleMonth(startOfMonth(presetDay));
                }}
                accessibilityRole="button"
                accessibilityLabel={preset.label}
                style={[
                  styles.preset,
                  {
                    borderColor: active ? c.brand : c.border,
                    backgroundColor: active
                      ? withAlpha(c.brand, 0.1)
                      : "transparent",
                  },
                ]}
              >
                <Text
                  style={[
                    styles.presetLabel,
                    { color: active ? c.brand : c.foreground },
                  ]}
                >
                  {preset.label}
                </Text>
              </Pressable>
            );
          })}
        </View>

        {/* Month navigation */}
        <View style={styles.monthRow}>
          <Pressable
            onPress={() => setVisibleMonth((m) => addMonths(m, -1))}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel="Previous month"
            style={styles.monthButton}
          >
            <Icon name="chevron-back" size={16} color={c.mutedForeground} />
          </Pressable>
          <Text style={[styles.monthLabel, { color: c.foreground }]}>
            {monthLabel}
          </Text>
          <Pressable
            onPress={() => setVisibleMonth((m) => addMonths(m, 1))}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel="Next month"
            style={styles.monthButton}
          >
            <Icon name="chevron-forward" size={16} color={c.mutedForeground} />
          </Pressable>
        </View>

        {/* Weekday header */}
        <View style={styles.grid}>
          {WEEKDAY_LABELS.map((label, i) => (
            <View key={`wd-${i}`} style={styles.cell}>
              <Text style={[styles.weekday, { color: c.mutedForeground }]}>
                {label}
              </Text>
            </View>
          ))}
          {cells.map((date, i) => {
            if (!date) return <View key={`pad-${i}`} style={styles.cell} />;
            const selected = isSameDay(date, draft);
            const isToday = isSameDay(date, todayPlus(0));
            return (
              <Pressable
                key={date.toISOString()}
                onPress={() => setDraft(date)}
                accessibilityRole="button"
                accessibilityLabel={toDateOnly(date)}
                accessibilityState={{ selected }}
                style={[
                  styles.cell,
                  selected
                    ? { backgroundColor: c.brand }
                    : isToday
                      ? { backgroundColor: withAlpha(c.brand, 0.1) }
                      : null,
                ]}
              >
                <Text
                  style={[
                    styles.dayLabel,
                    {
                      color: selected
                        ? c.primaryForeground
                        : c.foreground,
                      fontWeight: selected || isToday ? "600" : "400",
                    },
                  ]}
                >
                  {date.getDate()}
                </Text>
              </Pressable>
            );
          })}
        </View>
      </View>
    );
  },
);

/**
 * The iOS formSheet route (`issue/[id]/picker/due-date.tsx`): UIDatePicker
 * needs a confirmation step, so the route rendered Done / Clear in a mini
 * header inside the body — the sheet replicates that header row and hosts
 * the calendar body behind a ref handle.
 */
export function IssueDueDatePickerSheet({
  issueId,
  visible,
  onClose,
}: {
  issueId: string;
  visible: boolean;
  onClose: () => void;
}) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const updateIssue = useUpdateIssue(issueId);
  const bodyRef = useRef<DueDatePickerBodyHandle>(null);

  const value = issue?.due_date ?? null;

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View style={styles.sheetHeader}>
        <Text style={[styles.sheetTitle, { color: c.foreground }]}>
          Due date
        </Text>
        <View style={styles.sheetActions}>
          {value ? (
            <Pressable
              onPress={() => {
                updateIssue.mutate({ due_date: null });
                onClose();
              }}
              hitSlop={6}
              accessibilityRole="button"
              accessibilityLabel="Clear due date"
              style={({ pressed }) => [
                styles.sheetAction,
                pressed ? { backgroundColor: c.secondary } : null,
              ]}
            >
              <Text style={{ color: c.destructive, fontSize: 14 }}>Clear</Text>
            </Pressable>
          ) : null}
          <Pressable
            onPress={() => {
              const iso = bodyRef.current?.getIso();
              if (iso) updateIssue.mutate({ due_date: iso });
              onClose();
            }}
            hitSlop={6}
            accessibilityRole="button"
            accessibilityLabel="Set due date"
            style={({ pressed }) => [
              styles.sheetAction,
              pressed ? { backgroundColor: c.secondary } : null,
            ]}
          >
            <Text style={{ color: c.primary, fontSize: 14, fontWeight: "500" }}>
              Done
            </Text>
          </Pressable>
        </View>
      </View>
      <DueDatePickerBody ref={bodyRef} value={value} />
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  // flex-1 items-center pt-2 → body centers with side padding
  wrap: { paddingHorizontal: 16, paddingTop: 8, paddingBottom: 4 },  // presets row
  presets: { flexDirection: "row", gap: 8, marginBottom: 8 },
  // px-3 py-1.5 rounded-full border
  preset: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 999,
    borderWidth: 1,
  },
  // text-sm
  presetLabel: { fontSize: 13 },
  // month header row
  monthRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    marginBottom: 4,
  },
  monthButton: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
  },
  // text-base font-semibold
  monthLabel: { fontSize: 15, fontWeight: "600" },
  // 7-column grid
  grid: {
    flexDirection: "row",
    flexWrap: "wrap",
  },
  cell: {
    width: `${100 / 7}%` as `${number}%`,
    aspectRatio: 1.15,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 999,
  },
  // text-sm
  weekday: { fontSize: 12, fontWeight: "500" },
  dayLabel: { fontSize: 14 },
  // Done / Clear header row (the iOS route's mini header)
  sheetHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
  },
  // text-base font-semibold
  sheetTitle: { fontSize: 16, fontWeight: "600" },
  sheetActions: { flexDirection: "row", alignItems: "center", gap: 4 },
  sheetAction: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
  },
});
