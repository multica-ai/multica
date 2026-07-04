"use client";

import { useMilestones, useCreateMilestone, useDeleteMilestone } from "@multica/core/milestones";
import { Button } from "@multica/ui/components/ui/button";
import { format } from "date-fns";

export function ProjectMilestonesTab({ projectId }: { projectId: string }) {
  const { data: milestones = [] } = useMilestones(projectId);
  const createMut = useCreateMilestone(projectId);
  const deleteMut = useDeleteMilestone(projectId);

  return (
    <div className="p-6 space-y-6 max-w-4xl">
      <div className="flex items-center justify-between">
        <div className="flex flex-col gap-1">
          <h3 className="font-medium">Milestones</h3>
          <p className="text-sm text-muted-foreground">Track project phases and deadlines.</p>
        </div>
        <Button onClick={() => createMut.mutate({ title: "New Milestone", status: "active" })}>
          Create Milestone
        </Button>
      </div>

      <div className="grid gap-4">
        {milestones.map((m) => (
          <div key={m.id} className="border rounded-lg p-4 flex items-start justify-between bg-card">
            <div className="space-y-1">
              <h4 className="font-medium">{m.title}</h4>
              <p className="text-sm text-muted-foreground">{m.description || "No description provided."}</p>
              <div className="flex items-center gap-4 text-xs text-muted-foreground mt-2">
                <span className="capitalize px-2 py-0.5 rounded-full bg-muted border">
                  {m.status.replace("_", " ")}
                </span>
                {m.start_date && m.due_date && (
                  <span>{format(new Date(m.start_date), "MMM d, yyyy")} - {format(new Date(m.due_date), "MMM d, yyyy")}</span>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Button variant="ghost" size="sm" onClick={() => deleteMut.mutate(m.id)}>
                Delete
              </Button>
            </div>
          </div>
        ))}
        {milestones.length === 0 && (
          <div className="p-12 text-center border rounded-lg border-dashed">
            <p className="text-sm text-muted-foreground mb-4">No milestones created yet.</p>
            <Button variant="outline" onClick={() => createMut.mutate({ title: "Phase 1: Design", status: "active" })}>
              Create your first milestone
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
