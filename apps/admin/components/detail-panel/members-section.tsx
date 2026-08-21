import { Avatar, AvatarFallback } from "@multica/ui/components/ui/avatar";
import type { WorkspaceMember } from "@/lib/types";

function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const first = words[0];
  if (!first) return "?";
  const last = words[words.length - 1];
  if (words.length === 1 || !last) return first.slice(0, 2).toUpperCase();
  return (first.charAt(0) + last.charAt(0)).toUpperCase();
}

export function MembersSection({ members }: { members: WorkspaceMember[] }) {
  return (
    <section>
      <h3 className="mb-3 text-label font-medium text-muted-foreground uppercase tracking-wide">
        Members
      </h3>
      {members.length === 0 ? (
        <p className="text-body text-muted-foreground">No members.</p>
      ) : (
        <ul className="flex flex-wrap gap-1.5">
          {members.map((m) => (
            <li
              key={m.id}
              className="flex items-center gap-1.5 rounded-full bg-muted py-0.5 pl-0.5 pr-2 text-caption text-foreground"
            >
              <Avatar size="sm">
                <AvatarFallback>{initials(m.name)}</AvatarFallback>
              </Avatar>
              {m.name}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
