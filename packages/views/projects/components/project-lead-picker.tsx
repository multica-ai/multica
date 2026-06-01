"use client";

import { useState } from "react";
import { UserMinus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import type { Project, UpdateProjectRequest } from "@multica/core/types";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { ActorAvatar } from "../../common/actor-avatar";

export function ProjectLeadPicker({ project, handleUpdate, renderTrigger, align = "start" }: {
  project: Project;
  handleUpdate: (data: UpdateProjectRequest) => void;
  renderTrigger: (leadName: string | null) => React.ReactElement;
  align?: "start" | "end" | "center"
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const [leadOpen, setLeadOpen] = useState(false);
  const [leadFilter, setLeadFilter] = useState("");
  const leadQuery = leadFilter.toLowerCase();

  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(leadQuery) || matchesPinyin(m.name, leadQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && (a.name.toLowerCase().includes(leadQuery) || matchesPinyin(a.name, leadQuery)));

  const leadId = project.lead_id;
  const leadType = project.lead_type;
  const leadName = leadId && leadType ? getActorName(leadType, leadId) : null;

  return (
    <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
      <PopoverTrigger render={renderTrigger(leadName)} />
      <PopoverContent align={align} className="w-52 p-0">
        <div className="px-2 py-1.5 border-b">
          <input
            type="text"
            value={leadFilter}
            onChange={(e) => setLeadFilter(e.target.value)}
            placeholder={t(($) => $.lead.assign_placeholder)}
            className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          />
        </div>
        <div className="p-1 max-h-48 overflow-y-auto">
          <button
            type="button"
            onClick={() => { handleUpdate({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
          >
            <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
          </button>
          {filteredMembers.length > 0 && (
            <>
              <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.members_group)}</div>
              {filteredMembers.map((m) => (
                <button
                  type="button"
                  key={m.user_id}
                  onClick={() => { handleUpdate({ lead_type: "member", lead_id: m.user_id }); setLeadOpen(false); }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                  <span>{m.name}</span>
                </button>
              ))}
            </>
          )}
          {filteredAgents.length > 0 && (
            <>
              <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.agents_group)}</div>
              {filteredAgents.map((a) => (
                <button
                  type="button"
                  key={a.id}
                  onClick={() => { handleUpdate({ lead_type: "agent", lead_id: a.id }); setLeadOpen(false); }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <ActorAvatar actorType="agent" actorId={a.id} size={16} showStatusDot />
                  <span>{a.name}</span>
                </button>
              ))}
            </>
          )}
          {filteredMembers.length === 0 && filteredAgents.length === 0 && leadFilter && (
            <div className="px-2 py-3 text-center text-sm text-muted-foreground">{t(($) => $.lead.no_results)}</div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
