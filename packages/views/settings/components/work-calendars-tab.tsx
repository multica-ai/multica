"use client";

import { useState, useCallback, useMemo, useRef } from "react";
import {
    CalendarDays,
    Plus,
    Upload,
    Trash2,
    FileText,
    ChevronRight,
    ChevronLeft,
    Clock,
    Save,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
    DialogClose,
} from "@multica/ui/components/ui/dialog";
import {
    AlertDialog,
    AlertDialogContent,
    AlertDialogHeader,
    AlertDialogTitle,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogCancel,
    AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";

import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { workCalendarListOptions } from "@multica/core/work-calendars/queries";
import {
    useCreateWorkCalendar,
    useUpdateWorkCalendar,
    useDeleteWorkCalendar,
    useImportWorkCalendarFromPDF,
} from "@multica/core/work-calendars/mutations";
import type {
    WorkCalendar,
    CalendarDay,
    CalendarDayType,
    MonthlyHours,
} from "@multica/core/types";
import { useT } from "../../i18n";

// ---- Constants ----

const SHORT_MONTH_NAMES = [
    "Jan", "Feb", "Mar", "Apr", "May", "Jun",
    "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

const DAY_HEADERS = ["M", "T", "W", "T", "F", "S", "S"];

const DAY_TYPE_CONFIG: Record<
    CalendarDayType,
    { label: string; bg: string; text: string; dot: string; ring: string }
> = {
    normal: {
        label: "Normal",
        bg: "bg-emerald-500/12",
        text: "text-emerald-700 dark:text-emerald-400",
        dot: "bg-emerald-500",
        ring: "ring-emerald-500/40",
    },
    reduced: {
        label: "Reduced",
        bg: "bg-amber-500/12",
        text: "text-amber-700 dark:text-amber-400",
        dot: "bg-amber-500",
        ring: "ring-amber-500/40",
    },
    holiday: {
        label: "Holiday",
        bg: "bg-rose-500/12",
        text: "text-rose-700 dark:text-rose-400",
        dot: "bg-rose-500",
        ring: "ring-rose-500/40",
    },
    weekend: {
        label: "Weekend",
        bg: "bg-muted/50",
        text: "text-muted-foreground/50",
        dot: "bg-muted-foreground/30",
        ring: "ring-muted-foreground/30",
    },
};

const ALL_DAY_TYPES: CalendarDayType[] = ["normal", "reduced", "holiday", "weekend"];

// ---- Helpers ----

function getDaysInMonth(year: number, month: number) {
    return new Date(year, month, 0).getDate();
}

function getFirstDayOfMonth(year: number, month: number) {
    const day = new Date(year, month - 1, 1).getDay();
    return day === 0 ? 6 : day - 1;
}

function formatHours(hours: number): string {
    if (hours === 0) return "—";
    const h = Math.floor(hours);
    const m = Math.round((hours - h) * 60);
    if (m === 0) return `${h}h`;
    return `${h}h${m}`;
}

function buildDayMap(days: CalendarDay[]): Map<string, CalendarDay> {
    const map = new Map<string, CalendarDay>();
    for (const d of days) map.set(d.date, d);
    return map;
}

function computeMonthlyHoursFromDays(days: CalendarDay[]): MonthlyHours[] {
    const totals = new Map<number, number>();
    for (const d of days) {
        const month = parseInt(d.date.split("-")[1]!, 10);
        totals.set(month, (totals.get(month) ?? 0) + d.hours);
    }
    return Array.from(totals.entries())
        .sort(([a], [b]) => a - b)
        .map(([month, total_hours]) => ({ month, total_hours: Math.round(total_hours * 100) / 100 }));
}

// ---- Sub-components ----

/** Compact month grid for list view (read-only) */
function MiniMonthGrid({
    year,
    month,
    dayMap,
}: {
    year: number;
    month: number;
    dayMap: Map<string, CalendarDay>;
}) {
    const daysInMonth = getDaysInMonth(year, month);
    const startDay = getFirstDayOfMonth(year, month);
    const cells: (CalendarDay | null)[] = [];
    for (let i = 0; i < startDay; i++) cells.push(null);
    for (let d = 1; d <= daysInMonth; d++) {
        const dateStr = `${year}-${String(month).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
        cells.push(dayMap.get(dateStr) ?? null);
    }

    return (
        <div className="grid grid-cols-7 gap-px">
            {DAY_HEADERS.map((d, i) => (
                <div key={i} className="text-[9px] text-center text-muted-foreground/60 font-medium pb-0.5">{d}</div>
            ))}
            {cells.map((day, idx) =>
                !day ? (
                    <div key={`e-${idx}`} className="aspect-square" />
                ) : (
                    <div
                        key={day.date}
                        className={`aspect-square rounded-[3px] flex items-center justify-center text-[9px] tabular-nums font-medium ${day.type === "holiday"
                                ? "bg-rose-500/15 text-rose-600 dark:text-rose-400"
                                : day.type === "reduced"
                                    ? "bg-amber-500/15 text-amber-600 dark:text-amber-400"
                                    : day.type === "weekend"
                                        ? "bg-muted/50 text-muted-foreground/50"
                                        : "text-foreground/80"
                            }`}
                    >
                        {new Date(day.date).getDate()}
                    </div>
                ),
            )}
        </div>
    );
}

/** Interactive month grid for detail/edit view */
function EditableMonthGrid({
    year,
    month,
    dayMap,
    selectedDate,
    onSelectDate,
    onDayUpdate,
    monthlyHours,
}: {
    year: number;
    month: number;
    dayMap: Map<string, CalendarDay>;
    selectedDate: string | null;
    onSelectDate: (date: string | null) => void;
    onDayUpdate: (updated: CalendarDay) => void;
    monthlyHours?: MonthlyHours;
}) {
    const daysInMonth = getDaysInMonth(year, month);
    const startDay = getFirstDayOfMonth(year, month);
    const cells: (CalendarDay | null)[] = [];
    for (let i = 0; i < startDay; i++) cells.push(null);
    for (let d = 1; d <= daysInMonth; d++) {
        const dateStr = `${year}-${String(month).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
        cells.push(dayMap.get(dateStr) ?? null);
    }

    const { t } = useT("work-calendars");
    const monthNames = [
        t($ => $.month_1), t($ => $.month_2), t($ => $.month_3), t($ => $.month_4),
        t($ => $.month_5), t($ => $.month_6), t($ => $.month_7), t($ => $.month_8),
        t($ => $.month_9), t($ => $.month_10), t($ => $.month_11), t($ => $.month_12),
    ];
    const dayTypeLabels = {
        normal: t($ => $.day_type_normal),
        reduced: t($ => $.day_type_reduced),
        holiday: t($ => $.day_type_holiday),
        weekend: t($ => $.day_type_weekend),
    };

    return (
        <div className="space-y-1.5">
            <div className="flex items-center justify-between">
                <span className="text-xs font-semibold">{monthNames[month - 1]}</span>
                {monthlyHours && (
                    <span className="text-[10px] tabular-nums text-muted-foreground font-medium">
                        {`${monthlyHours.total_hours}h`}
                    </span>
                )}
            </div>
            <div className="grid grid-cols-7 gap-0.5">
                {DAY_HEADERS.map((d, i) => (
                    <div key={i} className="text-[9px] text-center text-muted-foreground/60 font-medium pb-0.5">{d}</div>
                ))}
                {cells.map((day, idx) => {
                    if (!day) return <div key={`e-${idx}`} className="aspect-square" />;
                    const isSelected = day.date === selectedDate;
                    const cfg = DAY_TYPE_CONFIG[day.type];
                    return (
                        <Popover
                            key={day.date}
                            open={isSelected}
                            onOpenChange={(open) => {
                                if (!open) onSelectDate(null);
                            }}
                        >
                            <PopoverTrigger
                                onClick={() => onSelectDate(isSelected ? null : day.date)}
                                className={`aspect-square rounded-[4px] flex items-center justify-center text-[10px] tabular-nums font-medium transition-all cursor-pointer
                  ${cfg.bg} ${cfg.text}
                  ${isSelected ? `ring-2 ${cfg.ring} scale-110 z-10 shadow-sm` : "hover:ring-1 hover:ring-foreground/15"}
                `}
                                title={`${day.date} — ${dayTypeLabels[day.type]}${day.hours > 0 ? ` (${formatHours(day.hours)})` : ""}${day.label ? ` — ${day.label}` : ""}`}
                            >
                                {new Date(day.date).getDate()}
                            </PopoverTrigger>
                            {isSelected && (
                                <PopoverContent side="right" align="start" sideOffset={8} className="w-56 p-2.5">
                                    <DayEditorPopoverContent day={day} onUpdate={onDayUpdate} />
                                </PopoverContent>
                            )}
                        </Popover>
                    );
                })}
            </div>
        </div>
    );
}

/** Compact day editor for popover */
function DayEditorPopoverContent({
    day,
    onUpdate,
}: {
    day: CalendarDay;
    onUpdate: (updated: CalendarDay) => void;
}) {
    const { t } = useT("work-calendars");
    const cfg = DAY_TYPE_CONFIG[day.type];
    const dateObj = new Date(day.date + "T00:00:00");
    const dayName = dateObj.toLocaleDateString("en-US", { weekday: "short" });
    const monthDay = dateObj.toLocaleDateString("en-US", { month: "short", day: "numeric" });
    const dayTypeLabels = {
        normal: t($ => $.day_type_normal),
        reduced: t($ => $.day_type_reduced),
        holiday: t($ => $.day_type_holiday),
        weekend: t($ => $.day_type_weekend),
    };

    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between">
                <span className="text-xs font-medium">{dayName}, {monthDay}</span>
                <Badge variant="secondary" className={`text-[9px] py-0 px-1.5 ${cfg.text}`}>
                    <div className={`h-1.5 w-1.5 rounded-full ${cfg.dot}`} />
                    {dayTypeLabels[day.type]}
                </Badge>
            </div>

            {/* Day type selector */}
            <div className="grid grid-cols-2 gap-1">
                {ALL_DAY_TYPES.map((type) => {
                    const tc = DAY_TYPE_CONFIG[type];
                    const isActive = day.type === type;
                    return (
                        <button
                            key={type}
                            type="button"
                            onClick={() => {
                                const hours = type === "holiday" || type === "weekend" ? 0
                                    : type === "reduced" ? 7
                                        : 8.5;
                                onUpdate({ ...day, type, hours });
                            }}
                            className={`text-[10px] font-medium py-1 px-1.5 rounded transition-all text-center
                ${isActive
                                    ? `${tc.bg} ${tc.text} ring-1 ${tc.ring}`
                                    : "bg-muted/30 text-muted-foreground hover:bg-muted/50"
                                }
              `}
                        >
                            {dayTypeLabels[type]}
                        </button>
                    );
                })}
            </div>

            {/* Hours + Label */}
            <div className="flex items-center gap-2">
                <div className="w-16">
                    <Input
                        type="number"
                        min={0}
                        max={24}
                        step={0.25}
                        value={day.hours}
                        onChange={(e) => onUpdate({ ...day, hours: parseFloat(e.target.value) || 0 })}
                        className="h-6 text-[11px] tabular-nums px-1.5"
                    />
                </div>
                <Input
                    type="text"
                    placeholder={t($ => $.day_note_placeholder)}
                    value={day.label ?? ""}
                    onChange={(e) => onUpdate({ ...day, label: e.target.value || undefined })}
                    className="h-6 text-[11px] flex-1 px-1.5"
                />
            </div>
        </div>
    );
}

/** Stats bar */
function CalendarStats({ days, monthlyHours }: { days: CalendarDay[]; monthlyHours: MonthlyHours[] }) {
    const { t } = useT("work-calendars");
    const totalWorkHours = monthlyHours.reduce((s, m) => s + m.total_hours, 0);
    const holidays = days.filter((d) => d.type === "holiday").length;
    const reducedDays = days.filter((d) => d.type === "reduced").length;
    const normalDays = days.filter((d) => d.type === "normal").length;

    return (
        <div className="grid grid-cols-4 gap-2">
            {[
                { icon: <Clock className="h-3.5 w-3.5 text-muted-foreground" />, value: `${Math.round(totalWorkHours)}h`, label: t($ => $.stats_total_year), bg: "bg-muted/40" },
                { icon: <div className="h-2 w-2 rounded-full bg-emerald-500" />, value: String(normalDays), label: t($ => $.stats_work_days), bg: "bg-emerald-500/10" },
                { icon: <div className="h-2 w-2 rounded-full bg-amber-500" />, value: String(reducedDays), label: t($ => $.stats_reduced), bg: "bg-amber-500/10" },
                { icon: <div className="h-2 w-2 rounded-full bg-rose-500" />, value: String(holidays), label: t($ => $.stats_holidays), bg: "bg-rose-500/10" },
            ].map((s) => (
                <div key={s.label} className={`flex items-center gap-2 p-2 rounded-md ${s.bg}`}>
                    <div className="shrink-0">{s.icon}</div>
                    <div>
                        <div className="text-xs font-semibold tabular-nums">{s.value}</div>
                        <div className="text-[10px] text-muted-foreground">{s.label}</div>
                    </div>
                </div>
            ))}
        </div>
    );
}

/** Monthly hours bar chart */
function MonthlyHoursChart({ monthlyHours }: { monthlyHours: MonthlyHours[] }) {
    const { t } = useT("work-calendars");
    const monthNames = [
        t($ => $.month_1), t($ => $.month_2), t($ => $.month_3), t($ => $.month_4),
        t($ => $.month_5), t($ => $.month_6), t($ => $.month_7), t($ => $.month_8),
        t($ => $.month_9), t($ => $.month_10), t($ => $.month_11), t($ => $.month_12),
    ];
    const maxHours = Math.max(...monthlyHours.map((m) => m.total_hours), 1);
    return (
        <div className="flex items-end gap-1 h-20">
            {monthlyHours.map((m) => {
                const height = (m.total_hours / maxHours) * 100;
                return (
                    <div key={m.month} className="flex-1 flex flex-col items-center gap-1" title={`${monthNames[m.month - 1]}: ${m.total_hours}h`}>
                        <div className="w-full flex items-end justify-center" style={{ height: "64px" }}>
                            <div
                                className="w-full rounded-t-sm bg-primary/20 hover:bg-primary/30 transition-colors relative group/bar"
                                style={{ height: `${Math.max(height, 4)}%` }}
                            >
                                <div className="absolute -top-5 left-1/2 -translate-x-1/2 text-[9px] tabular-nums text-muted-foreground opacity-0 group-hover/bar:opacity-100 transition-opacity whitespace-nowrap">
                                    {`${m.total_hours}h`}
                                </div>
                            </div>
                        </div>
                        <span className="text-[9px] text-muted-foreground font-medium">{SHORT_MONTH_NAMES[m.month - 1]}</span>
                    </div>
                );
            })}
        </div>
    );
}

// ---- Detail view with editing ----

function CalendarDetailView({
    calendar,
    onClose,
}: {
    calendar: WorkCalendar;
    onClose: () => void;
}) {
    const { t } = useT("work-calendars");
    // Local editable state — starts from the calendar data
    const [editedDays, setEditedDays] = useState<Map<string, CalendarDay>>(() => buildDayMap(calendar.days));
    const [selectedDate, setSelectedDate] = useState<string | null>(null);
    const [isDirty, setIsDirty] = useState(false);
    const updateMutation = useUpdateWorkCalendar();

    // Recompute derived data from edits
    const currentDays = useMemo(() => {
        return calendar.days.map((d) => editedDays.get(d.date) ?? d);
    }, [calendar.days, editedDays]);

    const currentMonthlyHours = useMemo(
        () => (isDirty ? computeMonthlyHoursFromDays(currentDays) : calendar.monthly_hours),
        [isDirty, currentDays, calendar.monthly_hours],
    );
    const dayMap = useMemo(() => buildDayMap(currentDays), [currentDays]);
    const monthlyMap = useMemo(() => new Map(currentMonthlyHours.map((m) => [m.month, m])), [currentMonthlyHours]);

    const handleDayUpdate = useCallback((updated: CalendarDay) => {
        setEditedDays((prev) => {
            const next = new Map(prev);
            next.set(updated.date, updated);
            return next;
        });
        setIsDirty(true);
    }, []);

    const handleSave = async () => {
        const days = currentDays;
        const monthly = computeMonthlyHoursFromDays(days);
        try {
            await updateMutation.mutateAsync({
                calendarId: calendar.id,
                data: {
                    name: calendar.name,
                    year: calendar.year,
                    days,
                    monthly_hours: monthly,
                },
            });
            setIsDirty(false);
            toast.success(t($ => $.toast_saved));
        } catch {
            toast.error(t($ => $.toast_save_failed));
        }
    };

    return (
        <div className="space-y-5">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <Button variant="ghost" size="icon-sm" onClick={onClose}>
                        <ChevronLeft className="h-4 w-4" />
                    </Button>
                    <div>
                        <h2 className="text-sm font-semibold">{calendar.name}</h2>
                        <p className="text-xs text-muted-foreground">
                            {calendar.year} · {calendar.source === "pdf_import" ? t($ => $.detail_subtitle_pdf) : t($ => $.detail_subtitle_manual)} · {t($ => $.detail_subtitle_hint)}
                        </p>
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    {isDirty && (
                        <Button size="sm" onClick={handleSave} disabled={updateMutation.isPending}>
                            <Save className="h-3.5 w-3.5" />
                            {updateMutation.isPending ? t($ => $.detail_saving) : t($ => $.detail_save)}
                        </Button>
                    )}
                    <Badge variant="outline" className="text-xs">
                        <CalendarDays className="h-3 w-3" />
                        {calendar.year}
                    </Badge>
                </div>
            </div>

            {/* Stats */}
            <CalendarStats days={currentDays} monthlyHours={currentMonthlyHours} />

            {/* Legend */}
            <div className="flex items-center gap-4 flex-wrap">
                {ALL_DAY_TYPES.map((type) => {
                    const dayTypeLabels = {
                        normal: t($ => $.day_type_normal),
                        reduced: t($ => $.day_type_reduced),
                        holiday: t($ => $.day_type_holiday),
                        weekend: t($ => $.day_type_weekend),
                    };
                    return (
                        <div key={type} className="flex items-center gap-1.5">
                            <div className={`h-2 w-2 rounded-full ${DAY_TYPE_CONFIG[type].dot}`} />
                            <span className="text-[10px] text-muted-foreground">{dayTypeLabels[type]}</span>
                        </div>
                    );
                })}
            </div>

            {/* Monthly hours chart */}
            {currentMonthlyHours.length > 0 && (
                <Card>
                    <CardContent className="pt-4">
                        <p className="text-xs font-semibold mb-3">{t($ => $.section_monthly_hours)}</p>
                        <MonthlyHoursChart monthlyHours={currentMonthlyHours} />
                    </CardContent>
                </Card>
            )}

            {/* Full year calendar grid — interactive */}
            <Card>
                <CardContent className="pt-4">
                    <p className="text-xs font-semibold mb-4">{t($ => $.section_full_year)}</p>
                    <div className="grid grid-cols-3 gap-x-5 gap-y-4 sm:grid-cols-4">
                        {Array.from({ length: 12 }, (_, i) => i + 1).map((month) => (
                            <EditableMonthGrid
                                key={month}
                                year={calendar.year}
                                month={month}
                                dayMap={dayMap}
                                selectedDate={selectedDate}
                                onSelectDate={setSelectedDate}
                                onDayUpdate={handleDayUpdate}
                                monthlyHours={monthlyMap.get(month)}
                            />
                        ))}
                    </div>
                </CardContent>
            </Card>
        </div>
    );
}

// ---- List item card ----

function CalendarCard({
    calendar,
    onSelect,
    onDelete,
}: {
    calendar: WorkCalendar;
    onSelect: () => void;
    onDelete: () => void;
}) {
    const { t } = useT("work-calendars");
    const dayMap = buildDayMap(calendar.days);
    const totalHours = calendar.monthly_hours.reduce((s, m) => s + m.total_hours, 0);
    const holidays = calendar.days.filter((d) => d.type === "holiday").length;
    const currentMonth = new Date().getMonth() + 1;
    const previewMonth = calendar.year === new Date().getFullYear() ? currentMonth : 1;

    return (
        <Card
            className="group/card cursor-pointer hover:border-primary/30 transition-all duration-200 hover:shadow-sm"
            onClick={onSelect}
        >
            <CardContent className="space-y-3">
                <div className="flex items-start justify-between">
                    <div className="space-y-0.5 min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <h3 className="text-sm font-semibold truncate">{calendar.name}</h3>
                            {calendar.source === "pdf_import" && (
                                <Badge variant="secondary" className="text-[10px] shrink-0">
                                    <FileText className="h-2.5 w-2.5" />
                                    PDF
                                </Badge>
                            )}
                        </div>
                        <p className="text-xs text-muted-foreground">
                            {t($ => $.card_meta, { year: calendar.year, hours: Math.round(totalHours), holidays })}
                        </p>
                    </div>
                    <div className="flex items-center gap-1 shrink-0">
                        <Button
                            variant="ghost"
                            size="icon-sm"
                            className="opacity-0 group-hover/card:opacity-100 transition-opacity"
                            onClick={(e) => { e.stopPropagation(); onDelete(); }}
                        >
                            <Trash2 className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
                        </Button>
                        <ChevronRight className="h-4 w-4 text-muted-foreground" />
                    </div>
                </div>
                <div className="w-full max-w-[180px]">
                    <MiniMonthGrid year={calendar.year} month={previewMonth} dayMap={dayMap} />
                </div>
            </CardContent>
        </Card>
    );
}

// ---- Dialogs ----

function CreateCalendarDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
    const { t } = useT("work-calendars");
    const [name, setName] = useState("");
    const [year, setYear] = useState(new Date().getFullYear());
    const createMutation = useCreateWorkCalendar();

    const handleCreate = async () => {
        if (!name.trim()) { toast.error(t($ => $.toast_name_required)); return; }

        // Generate a full year of default days (weekends + 8h workdays)
        const days: CalendarDay[] = [];
        for (let month = 1; month <= 12; month++) {
            const daysInMonth = new Date(year, month, 0).getDate();
            for (let d = 1; d <= daysInMonth; d++) {
                const date = new Date(year, month - 1, d);
                const dateStr = `${year}-${String(month).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
                const isWeekend = date.getDay() === 0 || date.getDay() === 6;
                days.push({
                    date: dateStr,
                    type: isWeekend ? "weekend" : "normal",
                    hours: isWeekend ? 0 : 8,
                });
            }
        }
        const monthly_hours = computeMonthlyHoursFromDays(days);

        try {
            await createMutation.mutateAsync({ name: name.trim(), year, days, monthly_hours });
            toast.success(t($ => $.toast_created));
            setName("");
            onOpenChange(false);
        } catch {
            toast.error(t($ => $.toast_create_failed));
        }
    };

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>{t($ => $.create_dialog_title)}</DialogTitle>
                    <DialogDescription>
                        {t($ => $.create_dialog_description)}
                    </DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-2">
                    <div className="space-y-1.5">
                        <label className="text-xs text-muted-foreground font-medium">{t($ => $.form_label_name)}</label>
                        <Input placeholder={t($ => $.form_placeholder_name)} value={name} onChange={(e) => setName(e.target.value)} className="text-sm" autoFocus />
                    </div>
                    <div className="space-y-1.5">
                        <label className="text-xs text-muted-foreground font-medium">{t($ => $.form_label_year)}</label>
                        <Input type="number" min={2000} max={2100} value={year} onChange={(e) => setYear(parseInt(e.target.value) || new Date().getFullYear())} className="text-sm" />
                    </div>
                </div>
                <DialogFooter>
                    <DialogClose render={<Button variant="outline" size="sm">{t($ => $.btn_cancel)}</Button>} />
                    <Button size="sm" onClick={handleCreate} disabled={createMutation.isPending}>
                        {createMutation.isPending ? t($ => $.btn_creating) : t($ => $.btn_create)}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function ImportPDFDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
    const { t } = useT("work-calendars");
    const [name, setName] = useState("");
    const [file, setFile] = useState<File | null>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);
    const importMutation = useImportWorkCalendarFromPDF();

    const handleImport = async () => {
        if (!file) { toast.error(t($ => $.toast_select_pdf)); return; }
        if (!name.trim()) { toast.error(t($ => $.toast_name_required)); return; }
        try {
            await importMutation.mutateAsync({ file, name: name.trim() });
            toast.success(t($ => $.toast_imported));
            setFile(null);
            setName("");
            onOpenChange(false);
        } catch (err) {
            toast.error(err instanceof Error ? err.message : t($ => $.toast_import_failed));
        }
    };

    const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
        const selected = e.target.files?.[0];
        if (selected) {
            setFile(selected);
            if (!name) setName(selected.name.replace(/\.pdf$/i, "").replace(/[_-]/g, " "));
        }
    }, [name]);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>{t($ => $.import_dialog_title)}</DialogTitle>
                    <DialogDescription>
                        {t($ => $.import_dialog_description)}
                    </DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-2">
                    <div className="space-y-1.5">
                        <label className="text-xs text-muted-foreground font-medium">{t($ => $.form_label_name)}</label>
                        <Input placeholder={t($ => $.form_placeholder_import_name)} value={name} onChange={(e) => setName(e.target.value)} className="text-sm" />
                    </div>
                    <div className="space-y-1.5">
                        <label className="text-xs text-muted-foreground font-medium">{t($ => $.form_label_pdf_file)}</label>
                        <input ref={fileInputRef} type="file" accept=".pdf,application/pdf" onChange={handleFileChange} className="hidden" />
                        <div
                            onClick={() => fileInputRef.current?.click()}
                            className="border border-dashed rounded-lg p-6 text-center cursor-pointer hover:border-primary/50 hover:bg-accent/30 transition-colors"
                        >
                            {file ? (
                                <div className="flex items-center justify-center gap-2">
                                    <FileText className="h-5 w-5 text-primary" />
                                    <div className="text-left">
                                        <p className="text-sm font-medium truncate max-w-[250px]">{file.name}</p>
                                        <p className="text-xs text-muted-foreground">{(file.size / 1024).toFixed(1)} {t($ => $.file_click_to_change)}</p>
                                    </div>
                                </div>
                            ) : (
                                <div className="space-y-2">
                                    <Upload className="h-8 w-8 text-muted-foreground/50 mx-auto" />
                                    <p className="text-sm text-muted-foreground">{t($ => $.file_click_to_select)}</p>
                                    <p className="text-xs text-muted-foreground/60">{t($ => $.file_max_size)}</p>
                                </div>
                            )}
                        </div>
                    </div>
                </div>
                <DialogFooter>
                    <DialogClose render={<Button variant="outline" size="sm">{t($ => $.btn_cancel)}</Button>} />
                    <Button size="sm" onClick={handleImport} disabled={importMutation.isPending || !file}>
                        <Upload className="h-3.5 w-3.5" />
                        {importMutation.isPending ? t($ => $.btn_importing) : t($ => $.btn_import)}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

// ---- Main component ----

export function WorkCalendarsTab() {
    const wsId = useWorkspaceId();
    const { t } = useT("work-calendars");
    const { data, isLoading } = useQuery(workCalendarListOptions(wsId));
    const deleteMutation = useDeleteWorkCalendar();
    const [selectedCalendar, setSelectedCalendar] = useState<WorkCalendar | null>(null);
    const [showCreateDialog, setShowCreateDialog] = useState(false);
    const [showImportDialog, setShowImportDialog] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState<WorkCalendar | null>(null);

    const calendars = data?.calendars ?? [];

    const handleDelete = async () => {
        if (!deleteTarget) return;
        try {
            await deleteMutation.mutateAsync(deleteTarget.id);
            toast.success(t($ => $.toast_deleted));
            if (selectedCalendar?.id === deleteTarget.id) setSelectedCalendar(null);
        } catch {
            toast.error(t($ => $.toast_delete_failed));
        }
        setDeleteTarget(null);
    };

    // Detail/edit view
    if (selectedCalendar) {
        const fresh = calendars.find((c) => c.id === selectedCalendar.id) ?? selectedCalendar;
        return <CalendarDetailView calendar={fresh} onClose={() => setSelectedCalendar(null)} />;
    }

    // List view
    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                    <h2 className="text-sm font-semibold">{t($ => $.tab_title)}</h2>
                    <p className="text-xs text-muted-foreground">
                        {t($ => $.tab_description)}
                    </p>
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={() => setShowImportDialog(true)}>
                        <Upload className="h-3.5 w-3.5" />
                        {t($ => $.btn_import_pdf)}
                    </Button>
                    <Button size="sm" onClick={() => setShowCreateDialog(true)}>
                        <Plus className="h-3.5 w-3.5" />
                        {t($ => $.btn_new_calendar)}
                    </Button>
                </div>
            </div>

            {isLoading ? (
                <div className="space-y-3">
                    <Skeleton className="h-[140px] w-full rounded-lg" />
                    <Skeleton className="h-[140px] w-full rounded-lg" />
                </div>
            ) : calendars.length === 0 ? (
                <Card>
                    <CardContent className="py-12 text-center">
                        <div className="mx-auto w-12 h-12 rounded-full bg-primary/10 flex items-center justify-center mb-4">
                            <CalendarDays className="h-6 w-6 text-primary" />
                        </div>
                        <h3 className="text-sm font-semibold mb-1">{t($ => $.empty_title)}</h3>
                        <p className="text-xs text-muted-foreground mb-4 max-w-[300px] mx-auto">
                            {t($ => $.empty_description)}
                        </p>
                        <div className="flex items-center justify-center gap-2">
                            <Button variant="outline" size="sm" onClick={() => setShowImportDialog(true)}>
                                <Upload className="h-3.5 w-3.5" />
                                {t($ => $.empty_btn_import)}
                            </Button>
                            <Button size="sm" onClick={() => setShowCreateDialog(true)}>
                                <Plus className="h-3.5 w-3.5" />
                                {t($ => $.empty_btn_create)}
                            </Button>
                        </div>
                    </CardContent>
                </Card>
            ) : (
                <div className="space-y-3">
                    {calendars.map((cal) => (
                        <CalendarCard key={cal.id} calendar={cal} onSelect={() => setSelectedCalendar(cal)} onDelete={() => setDeleteTarget(cal)} />
                    ))}
                </div>
            )}

            <CreateCalendarDialog open={showCreateDialog} onOpenChange={setShowCreateDialog} />
            <ImportPDFDialog open={showImportDialog} onOpenChange={setShowImportDialog} />

            <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t($ => $.delete_dialog_title)}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t($ => $.delete_dialog_description, { name: deleteTarget?.name ?? "" })}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>{t($ => $.delete_btn_cancel)}</AlertDialogCancel>
                        <AlertDialogAction onClick={handleDelete} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                            {deleteMutation.isPending ? t($ => $.delete_btn_confirming) : t($ => $.delete_btn_confirm)}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </div>
    );
}
