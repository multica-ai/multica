"use client";

import { useState } from "react";
import { CalendarDays } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { Calendar } from "@multica/ui/components/ui/calendar";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  Sheet,
  SheetTrigger,
  SheetContent,
} from "@multica/ui/components/ui/sheet";
import { Button } from "@multica/ui/components/ui/button";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useT } from "../../../i18n";

export function DueDatePicker({
  dueDate,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  align = "start",
  defaultOpen = false,
}: {
  dueDate: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  /** Open the popover on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(defaultOpen);
  const isMobile = useIsMobile();
  const date = dueDate ? new Date(dueDate) : undefined;
  const isOverdue = date ? date < new Date() : false;

  const triggerContent = customTrigger ?? (
    <>
      <CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />
      {date ? (
        <span className={isOverdue ? "text-destructive" : ""}>
          {date.toLocaleDateString("en-US", { month: "short", day: "numeric" })}
        </span>
      ) : (
        <span className="text-muted-foreground">{t(($) => $.pickers.due_date.trigger_label)}</span>
      )}
    </>
  );

  const calendarBody = (
    <>
      <Calendar
        mode="single"
        selected={date}
        onSelect={(d: Date | undefined) => {
          onUpdate({ due_date: d ? d.toISOString() : null });
          setOpen(false);
        }}
      />
      {date && (
        <div className="border-t px-3 py-2">
          <Button
            variant="ghost"
            size="xs"
            onClick={() => {
              onUpdate({ due_date: null });
              setOpen(false);
            }}
            className="text-muted-foreground hover:text-foreground"
          >
            {t(($) => $.pickers.due_date.clear_action)}
          </Button>
        </div>
      )}
    </>
  );

  if (isMobile) {
    return (
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger
          className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors"}
          render={triggerRender}
        >
          {triggerContent}
        </SheetTrigger>
        <SheetContent
          side="bottom"
          showCloseButton={false}
          className="rounded-t-xl pb-[env(safe-area-inset-bottom)] items-center"
        >
          <div className="mx-auto mt-2 h-1 w-10 shrink-0 rounded-full bg-muted-foreground/30" />
          {calendarBody}
        </SheetContent>
      </Sheet>
    );
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors"}
        render={triggerRender}
      >
        {triggerContent}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        {calendarBody}
      </PopoverContent>
    </Popover>
  );
}
