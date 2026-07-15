"use client";

import type { IssueProperty } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  Bookmark,
  BriefcaseBusiness,
  Bug,
  CalendarDays,
  CircleAlert,
  CircleCheck,
  CircleDot,
  Clock3,
  Code2,
  Database,
  Flag,
  FolderKanban,
  Gauge,
  Globe2,
  Hash,
  Heart,
  Layers3,
  Lightbulb,
  Link,
  ListChecks,
  LockKeyhole,
  MapPin,
  Megaphone,
  Milestone,
  Package,
  Palette,
  Rocket,
  Shapes,
  Shield,
  SignalHigh,
  Sparkles,
  Star,
  Tag,
  Target,
  UserRound,
  Wrench,
  X,
  Zap,
  type LucideIcon,
} from "lucide-react";

interface PropertyIconOption {
  value: string;
  label: string;
  Icon: LucideIcon;
}

/**
 * A deliberately small Lucide catalog that starts with the same symbols used
 * by built-in issue properties. Persist stable keys rather than SVG data or
 * component names so the visual implementation can evolve independently.
 */
export const PROPERTY_ICON_OPTIONS = [
  { value: "circle-dot", label: "Status", Icon: CircleDot },
  { value: "signal-high", label: "Priority", Icon: SignalHigh },
  { value: "user-round", label: "Assignee", Icon: UserRound },
  { value: "folder-kanban", label: "Project", Icon: FolderKanban },
  { value: "calendar-days", label: "Date", Icon: CalendarDays },
  { value: "tag", label: "Label", Icon: Tag },
  { value: "milestone", label: "Milestone", Icon: Milestone },
  { value: "flag", label: "Flag", Icon: Flag },
  { value: "bookmark", label: "Bookmark", Icon: Bookmark },
  { value: "star", label: "Star", Icon: Star },
  { value: "target", label: "Target", Icon: Target },
  { value: "shield", label: "Shield", Icon: Shield },
  { value: "bug", label: "Bug", Icon: Bug },
  { value: "zap", label: "Lightning", Icon: Zap },
  { value: "rocket", label: "Rocket", Icon: Rocket },
  { value: "sparkles", label: "Sparkles", Icon: Sparkles },
  { value: "lightbulb", label: "Idea", Icon: Lightbulb },
  { value: "globe-2", label: "Globe", Icon: Globe2 },
  { value: "link", label: "Link", Icon: Link },
  { value: "hash", label: "Number", Icon: Hash },
  { value: "list-checks", label: "Checklist", Icon: ListChecks },
  { value: "circle-check", label: "Complete", Icon: CircleCheck },
  { value: "clock-3", label: "Time", Icon: Clock3 },
  { value: "briefcase-business", label: "Work", Icon: BriefcaseBusiness },
  { value: "layers-3", label: "Layers", Icon: Layers3 },
  { value: "gauge", label: "Gauge", Icon: Gauge },
  { value: "database", label: "Database", Icon: Database },
  { value: "code-2", label: "Code", Icon: Code2 },
  { value: "palette", label: "Design", Icon: Palette },
  { value: "megaphone", label: "Announcement", Icon: Megaphone },
  { value: "map-pin", label: "Location", Icon: MapPin },
  { value: "package", label: "Package", Icon: Package },
  { value: "wrench", label: "Tools", Icon: Wrench },
  { value: "heart", label: "Favorite", Icon: Heart },
  { value: "circle-alert", label: "Alert", Icon: CircleAlert },
  { value: "lock-keyhole", label: "Private", Icon: LockKeyhole },
] satisfies PropertyIconOption[];

function findPropertyIcon(value: string | undefined) {
  return PROPERTY_ICON_OPTIONS.find((option) => option.value === value);
}

export function PropertyIconGlyph({
  icon,
  className,
}: {
  icon: string;
  className?: string;
}) {
  const Glyph = findPropertyIcon(icon)?.Icon ?? Shapes;

  return (
    <Glyph
      aria-hidden="true"
      data-property-icon={icon}
      className={cn("size-4 shrink-0", className)}
    />
  );
}

export function PropertyIcon({
  property,
  className,
}: {
  property?: Pick<IssueProperty, "icon"> | null;
  className?: string;
}) {
  if (!property?.icon) return null;
  return <PropertyIconGlyph icon={property.icon} className={className} />;
}

export function PropertyIconPicker({
  value,
  label,
  removeLabel,
  onSelect,
  onRemove,
}: {
  value: string;
  label: string;
  removeLabel: string;
  onSelect: (value: string) => void;
  onRemove: () => void;
}) {
  return (
    <div className="w-64">
      <div className="flex items-center justify-between px-1 pb-2">
        <span className="text-xs font-medium text-foreground">{label}</span>
        {value && (
          <span className="truncate pl-3 text-[11px] text-muted-foreground">
            {findPropertyIcon(value)?.label}
          </span>
        )}
      </div>
      <div className="grid grid-cols-6 gap-1" aria-label={label}>
        {PROPERTY_ICON_OPTIONS.map((option) => {
          const selected = option.value === value;
          return (
            <button
              key={option.value}
              type="button"
              aria-label={option.label}
              aria-pressed={selected}
              title={option.label}
              className={cn(
                "flex size-9 items-center justify-center rounded-md text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring",
                selected && "bg-brand/10 text-brand ring-1 ring-inset ring-brand/25",
              )}
              onClick={() => onSelect(option.value)}
            >
              <option.Icon className="size-4" />
            </button>
          );
        })}
      </div>
      {value && (
        <div className="mt-2 border-t border-surface-border pt-1.5">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="w-full justify-start text-muted-foreground"
            onClick={onRemove}
          >
            <X className="size-4" />
            {removeLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
