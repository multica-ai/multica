"use client";

import { useQuery } from "@tanstack/react-query";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import type { MemberWithUser } from "@multica/core/types";
import { useDashboardStore } from "../../core/store";

const EMPTY_MEMBERS: MemberWithUser[] = [];

interface MemberStripProps {
  wsId: string;
}

// Member cards. Real per-member activity arrives in Fase 2 (JEH-703); for
// now we render the roster with role + email so the Members tab has visible
// content distinct from the Agents tab.
export function MemberStrip({ wsId }: MemberStripProps) {
  const actorId = useDashboardStore((s) => s.actorId);
  const { data: members = EMPTY_MEMBERS, isLoading } = useQuery(
    memberListOptions(wsId),
  );

  const visible = actorId
    ? members.filter((m) => m.user_id === actorId || m.id === actorId)
    : members;

  if (isLoading) {
    return (
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Card key={i} className="h-24 animate-pulse" />
        ))}
      </div>
    );
  }

  if (visible.length === 0) {
    return (
      <Card>
        <CardContent className="py-8 text-center text-sm text-muted-foreground">
          Ingen members matcher filteret.
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {visible.slice(0, 8).map((m) => (
        <MemberCard key={m.id} member={m} />
      ))}
    </div>
  );
}

function MemberCard({ member }: { member: MemberWithUser }) {
  return (
    <Card>
      <CardContent className="space-y-2 p-3">
        <div className="flex items-center gap-2">
          <ActorAvatar
            name={member.name}
            initials={member.name.charAt(0).toUpperCase()}
            avatarUrl={member.avatar_url}
            size={20}
          />
          <span className="truncate text-sm font-medium">{member.name}</span>
        </div>
        <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
          {member.role}
        </p>
        <p className="line-clamp-1 text-xs text-muted-foreground">{member.email}</p>
      </CardContent>
    </Card>
  );
}
