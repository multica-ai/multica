"use client";

import { useState } from "react";
import { useProjectMembers, useAddProjectMember, useRemoveProjectMember, useUpdateProjectMember } from "@multica/core/projects/members";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useQuery } from "@tanstack/react-query";
import { ActorAvatar } from "../../common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";

export function ProjectMembersTab({ projectId }: { projectId: string }) {
  const { data: projectMembers = [] } = useProjectMembers(projectId);
  const wsId = useWorkspaceId();
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const addMut = useAddProjectMember(projectId);
  const updateMut = useUpdateProjectMember(projectId);
  const removeMut = useRemoveProjectMember(projectId);

  const [selectedMember, setSelectedMember] = useState<string>("");

  const handleAdd = () => {
    if (!selectedMember) return;
    addMut.mutate({ memberId: selectedMember, role: "viewer" }, {
      onSuccess: () => setSelectedMember("")
    });
  };

  const unaddedMembers = workspaceMembers.filter(wm => !projectMembers.some(pm => pm.member_id === wm.user_id));

  return (
    <div className="p-6 space-y-6 max-w-3xl">
      <div className="flex flex-col gap-1">
        <h3 className="font-medium">Project Members</h3>
        <p className="text-sm text-muted-foreground">Manage who has access to this project and their roles.</p>
      </div>

      <div className="flex items-center gap-2">
        <Select value={selectedMember} onValueChange={(val) => setSelectedMember(val || "")}>
          <SelectTrigger className="w-[250px]">
            <SelectValue placeholder="Select a workspace member..." />
          </SelectTrigger>
          <SelectContent>
            {unaddedMembers.map(m => (
              <SelectItem key={m.user_id} value={m.user_id}>
                <div className="flex items-center gap-2">
                  <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                  <span>{m.name}</span>
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button onClick={handleAdd} disabled={!selectedMember || addMut.isPending}>Add to Project</Button>
      </div>

      <div className="border rounded-md divide-y">
        {projectMembers.map((pm) => {
          const m = workspaceMembers.find(w => w.user_id === pm.member_id);
          return (
            <div key={pm.member_id} className="flex items-center justify-between p-4">
              <div className="flex items-center gap-3">
                <ActorAvatar actorType="member" actorId={pm.member_id} size={32} />
                <div className="flex flex-col">
                  <span className="text-sm font-medium">{m?.name || "Unknown"}</span>
                  <span className="text-xs text-muted-foreground">{m?.email}</span>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <Select
                  value={pm.role}
                  onValueChange={(val) => { if (val) updateMut.mutate({ memberId: pm.member_id, role: val }); }}
                >
                  <SelectTrigger className="w-[120px] h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="admin">Admin</SelectItem>
                    <SelectItem value="editor">Editor</SelectItem>
                    <SelectItem value="viewer">Viewer</SelectItem>
                  </SelectContent>
                </Select>
                <Button 
                  variant="ghost" 
                  size="sm" 
                  className="text-destructive"
                  onClick={() => removeMut.mutate(pm.member_id)}
                >
                  Remove
                </Button>
              </div>
            </div>
          );
        })}
        {projectMembers.length === 0 && (
          <div className="p-8 text-center text-sm text-muted-foreground">
            No members have been added to this project yet.
          </div>
        )}
      </div>
    </div>
  );
}
