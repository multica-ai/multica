import { Globe, Link, BookOpen, BarChart3, Code, Terminal, Folder, FileText, Database, Server, Rocket, Shield, type IconComponent } from "@multica/ui/icons";
import { LINK_ICON_KEYS } from "@multica/core/links";

const icons: Record<(typeof LINK_ICON_KEYS)[number], IconComponent> = {
  globe: Globe, link: Link, "book-open": BookOpen, "chart-bar": BarChart3, code: Code, terminal: Terminal, folder: Folder, "file-text": FileText, database: Database, server: Server, rocket: Rocket, shield: Shield,
};
export function LinkIcon({ icon }: { icon: string }) {
  const Icon = icons[icon as keyof typeof icons];
  if (Icon || !icon) { const Component = Icon ?? Globe; return <Component aria-hidden="true" className="size-4 shrink-0" />; }
  return <span aria-hidden="true" className="inline-flex size-4 shrink-0 items-center justify-center">{icon}</span>;
}
