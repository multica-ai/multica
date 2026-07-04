import {
  CheckSquare,
  Bug,
  Sparkles,
  BookOpen,
  Palette,
  FileText,
  Megaphone,
  Box,
  type LucideIcon,
} from "lucide-react";

const ICON_MAP: Record<string, LucideIcon> = {
  "check-square": CheckSquare,
  bug: Bug,
  sparkles: Sparkles,
  "book-open": BookOpen,
  palette: Palette,
  "file-text": FileText,
  megaphone: Megaphone,
};

export function IssueTypeIcon({
  icon,
  color,
  className = "h-4 w-4",
}: {
  icon: string;
  color?: string;
  className?: string;
}) {
  const IconComponent = ICON_MAP[icon] || Box;

  return (
    <IconComponent
      className={`${className} shrink-0`}
      style={color ? { color } : undefined}
    />
  );
}
