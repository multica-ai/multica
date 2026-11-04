import * as React from "react";
import { createPortal } from "react-dom";

import { ArrowLeftIcon, ArrowRightIcon } from "#icons/system-icons";
import { cn } from "#lib/utils";
import { getPortalContainer } from "../../lib/portal-container";

export interface DatePickerLabels {
  placeholder: string;
  previousMonth: string;
  nextMonth: string;
  clear: string;
  today: string;
  weekdayLabels: readonly [
    string,
    string,
    string,
    string,
    string,
    string,
    string
  ];
}

export interface DatePickerProps extends Omit<
  React.ButtonHTMLAttributes<HTMLButtonElement>,
  "onChange" | "value" | "type"
> {
  value?: string;
  onValueChange?: (value: string) => void;
  labels?: Partial<DatePickerLabels>;
  formatDisplayValue?: (date: Date) => string;
  formatMonthLabel?: (date: Date) => string;
}

const defaultLabels: DatePickerLabels = {
  placeholder: "Year / Month / Day",
  previousMonth: "Previous month",
  nextMonth: "Next month",
  clear: "Clear",
  today: "Today",
  weekdayLabels: ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
};

const DATE_PICKER_POPOVER_WIDTH_PX = 264;
const DATE_PICKER_POPOVER_HEIGHT_PX = 336;
const DATE_PICKER_POPOVER_GAP_PX = 6;
const DATE_PICKER_VIEWPORT_MARGIN_PX = 8;

function parseDateValue(value: string | undefined): Date | null {
  if (!value) {
    return null;
  }

  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) {
    return null;
  }

  return new Date(year, month - 1, day);
}

function formatDateValue(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function defaultDisplayValueFormatter(date: Date): string {
  return new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit"
  }).format(date);
}

function defaultMonthLabelFormatter(date: Date): string {
  return new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "long"
  }).format(date);
}

function isSameDate(left: Date | null, right: Date | null): boolean {
  return Boolean(
    left &&
    right &&
    left.getFullYear() === right.getFullYear() &&
    left.getMonth() === right.getMonth() &&
    left.getDate() === right.getDate()
  );
}

function buildMonthGrid(monthDate: Date): Date[] {
  const firstOfMonth = new Date(
    monthDate.getFullYear(),
    monthDate.getMonth(),
    1
  );
  const gridStart = new Date(firstOfMonth);
  gridStart.setDate(firstOfMonth.getDate() - firstOfMonth.getDay());

  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(gridStart);
    date.setDate(gridStart.getDate() + index);
    return date;
  });
}

function resolvePopoverPosition(rect: DOMRect): {
  top: number;
  left: number;
  width: number;
} {
  const width = Math.max(rect.width, DATE_PICKER_POPOVER_WIDTH_PX);
  const maxLeft = window.innerWidth - width - DATE_PICKER_VIEWPORT_MARGIN_PX;
  const left = Math.max(
    DATE_PICKER_VIEWPORT_MARGIN_PX,
    Math.min(rect.left, Math.max(DATE_PICKER_VIEWPORT_MARGIN_PX, maxLeft))
  );
  const belowTop = rect.bottom + DATE_PICKER_POPOVER_GAP_PX;
  const aboveTop =
    rect.top - DATE_PICKER_POPOVER_GAP_PX - DATE_PICKER_POPOVER_HEIGHT_PX;
  const canOpenBelow =
    belowTop + DATE_PICKER_POPOVER_HEIGHT_PX <=
    window.innerHeight - DATE_PICKER_VIEWPORT_MARGIN_PX;
  const top = canOpenBelow
    ? belowTop
    : Math.max(DATE_PICKER_VIEWPORT_MARGIN_PX, aboveTop);

  return { top, left, width };
}

function subscribeScrollableAncestors(
  trigger: HTMLElement,
  onScrollLike: () => void
): () => void {
  const cleanups: Array<() => void> = [];
  let element: HTMLElement | null = trigger.parentElement;

  while (element && element !== document.documentElement) {
    const { overflowX, overflowY } = window.getComputedStyle(element);
    if (
      overflowY === "auto" ||
      overflowY === "scroll" ||
      overflowY === "overlay" ||
      overflowX === "auto" ||
      overflowX === "scroll" ||
      overflowX === "overlay"
    ) {
      element.addEventListener("scroll", onScrollLike, { passive: true });
      const current = element;
      cleanups.push(() => current.removeEventListener("scroll", onScrollLike));
    }
    element = element.parentElement;
  }

  return () => cleanups.forEach((dispose) => dispose());
}

const DatePicker = React.forwardRef<HTMLButtonElement, DatePickerProps>(
  (
    {
      className,
      value,
      onValueChange,
      onClick,
      disabled,
      labels,
      formatDisplayValue = defaultDisplayValueFormatter,
      formatMonthLabel = defaultMonthLabelFormatter,
      ...props
    },
    ref
  ) => {
    const resolvedLabels: DatePickerLabels = {
      ...defaultLabels,
      ...labels,
      weekdayLabels: labels?.weekdayLabels ?? defaultLabels.weekdayLabels
    };
    const triggerRef = React.useRef<HTMLButtonElement | null>(null);
    const popoverRef = React.useRef<HTMLDivElement | null>(null);
    const selectedDate = React.useMemo(() => parseDateValue(value), [value]);
    const [isOpen, setIsOpen] = React.useState(false);
    const [visibleMonth, setVisibleMonth] = React.useState(
      () => selectedDate ?? new Date()
    );
    const [popoverPosition, setPopoverPosition] = React.useState<{
      top: number;
      left: number;
      width: number;
    } | null>(null);

    React.useEffect(() => {
      if (selectedDate) {
        setVisibleMonth(selectedDate);
      }
    }, [selectedDate]);

    const openPopover = React.useCallback(() => {
      const trigger = triggerRef.current;
      if (!trigger) {
        return;
      }
      setPopoverPosition(
        resolvePopoverPosition(trigger.getBoundingClientRect())
      );
      setIsOpen(true);
    }, []);

    React.useLayoutEffect(() => {
      if (!isOpen) {
        return;
      }

      const sync = () => {
        const element = triggerRef.current;
        if (!element) {
          return;
        }
        setPopoverPosition(
          resolvePopoverPosition(element.getBoundingClientRect())
        );
      };

      sync();
      const rafId = window.requestAnimationFrame(sync);
      return () => window.cancelAnimationFrame(rafId);
    }, [isOpen]);

    React.useEffect(() => {
      if (!isOpen) {
        return undefined;
      }

      const ac = new AbortController();
      const { signal } = ac;

      const handlePointerDown = (event: PointerEvent) => {
        const target = event.target as Node | null;
        if (
          target &&
          (triggerRef.current?.contains(target) ||
            popoverRef.current?.contains(target))
        ) {
          return;
        }
        setIsOpen(false);
      };

      const handleKeyDown = (event: KeyboardEvent) => {
        if (event.key === "Escape") {
          setIsOpen(false);
          triggerRef.current?.focus();
        }
      };

      const updatePosition = () => {
        const element = triggerRef.current;
        if (!element) {
          return;
        }
        setPopoverPosition(
          resolvePopoverPosition(element.getBoundingClientRect())
        );
      };

      document.addEventListener("pointerdown", handlePointerDown, { signal });
      document.addEventListener("keydown", handleKeyDown, { signal });
      window.addEventListener("resize", updatePosition, { signal });
      document.addEventListener(
        "wheel",
        () => window.requestAnimationFrame(updatePosition),
        {
          capture: true,
          passive: true,
          signal
        }
      );

      const unsubAncestors = triggerRef.current
        ? subscribeScrollableAncestors(triggerRef.current, updatePosition)
        : () => {};

      return () => {
        ac.abort();
        unsubAncestors();
      };
    }, [isOpen]);

    const today = React.useMemo(() => new Date(), []);
    const monthGrid = React.useMemo(
      () => buildMonthGrid(visibleMonth),
      [visibleMonth]
    );

    const displayValue = selectedDate
      ? formatDisplayValue(selectedDate)
      : resolvedLabels.placeholder;

    const selectDate = (date: Date) => {
      onValueChange?.(formatDateValue(date));
      setIsOpen(false);
      triggerRef.current?.focus();
    };

    return (
      <>
        <button
          ref={(node) => {
            triggerRef.current = node;
            if (typeof ref === "function") {
              ref(node);
            } else if (ref) {
              ref.current = node;
            }
          }}
          data-slot="date-picker"
          disabled={disabled}
          type="button"
          className={cn(
            "inline-flex h-8 min-h-8 w-full items-center justify-start rounded-md border border-transparent bg-[var(--workbench-field-bg)] px-3 text-left text-[13px] text-foreground transition-[background-color,border-color,box-shadow,color] outline-none hover:bg-muted/60 focus-visible:border-border focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground disabled:opacity-100",
            !selectedDate && "text-muted-foreground",
            className
          )}
          onClick={(event) => {
            onClick?.(event);
            if (disabled) {
              return;
            }
            if (isOpen) {
              setIsOpen(false);
            } else {
              openPopover();
            }
          }}
          {...props}
        >
          <span className="min-w-0 truncate">{displayValue}</span>
        </button>

        {isOpen && popoverPosition
          ? createPortal(
              <div
                ref={popoverRef}
                data-slot="date-picker-content"
                className="fixed z-50 flex min-w-[264px] max-w-[min(100vw-16px,320px)] flex-col overflow-hidden rounded-xl border border-border/70 bg-popover p-3 text-popover-foreground shadow-soft"
                style={{
                  top: popoverPosition.top,
                  left: popoverPosition.left,
                  width: popoverPosition.width,
                  zIndex: "var(--z-popover)"
                }}
              >
                <div className="mb-3 flex items-center justify-between gap-2">
                  <div className="text-[13px] font-semibold text-foreground">
                    {formatMonthLabel(visibleMonth)}
                  </div>
                  <div className="flex items-center gap-1">
                    <button
                      aria-label={resolvedLabels.previousMonth}
                      className="inline-flex h-7 w-7 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                      type="button"
                      onClick={() =>
                        setVisibleMonth(
                          (current) =>
                            new Date(
                              current.getFullYear(),
                              current.getMonth() - 1,
                              1
                            )
                        )
                      }
                    >
                      <ArrowLeftIcon size={16} />
                    </button>
                    <button
                      aria-label={resolvedLabels.nextMonth}
                      className="inline-flex h-7 w-7 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                      type="button"
                      onClick={() =>
                        setVisibleMonth(
                          (current) =>
                            new Date(
                              current.getFullYear(),
                              current.getMonth() + 1,
                              1
                            )
                        )
                      }
                    >
                      <ArrowRightIcon size={16} />
                    </button>
                  </div>
                </div>
                <div className="grid grid-cols-7 gap-1 text-center text-[11px] font-semibold text-muted-foreground">
                  {resolvedLabels.weekdayLabels.map((day) => (
                    <div key={day} className="py-1">
                      {day}
                    </div>
                  ))}
                </div>
                <div
                  className="mt-1 grid grid-cols-7 justify-items-center gap-1"
                  role="grid"
                >
                  {monthGrid.map((date) => {
                    const isSelected = isSameDate(date, selectedDate);
                    const isToday = isSameDate(date, today);
                    const inCurrentMonth =
                      date.getMonth() === visibleMonth.getMonth();

                    return (
                      <button
                        key={formatDateValue(date)}
                        aria-pressed={isSelected}
                        className={cn(
                          "inline-flex h-8 w-8 items-center justify-center rounded-sm text-[13px] transition-[background-color,color,box-shadow] hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
                          inCurrentMonth
                            ? "text-foreground"
                            : "text-muted-foreground",
                          isToday &&
                            "bg-primary/10 text-primary shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--color-primary)_20%,transparent)]",
                          isSelected &&
                            "bg-primary text-primary-foreground hover:bg-primary"
                        )}
                        role="gridcell"
                        type="button"
                        onClick={() => selectDate(date)}
                      >
                        {date.getDate()}
                      </button>
                    );
                  })}
                </div>
                <div className="mt-3 flex items-center justify-between gap-2 border-t border-border/70 pt-3">
                  <button
                    className="inline-flex min-h-8 items-center justify-center rounded-sm px-3 text-[13px] font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    type="button"
                    onClick={() => {
                      onValueChange?.("");
                      setIsOpen(false);
                      triggerRef.current?.focus();
                    }}
                  >
                    {resolvedLabels.clear}
                  </button>
                  <button
                    className="inline-flex min-h-8 items-center justify-center rounded-sm bg-primary px-3 text-[13px] font-medium text-primary-foreground transition-opacity hover:opacity-90"
                    type="button"
                    onClick={() => selectDate(today)}
                  >
                    {resolvedLabels.today}
                  </button>
                </div>
              </div>,
              getPortalContainer()
            )
          : null}
      </>
    );
  }
);

DatePicker.displayName = "DatePicker";

export { DatePicker };
