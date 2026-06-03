import { useQuery } from "@tanstack/react-query";
import { projectUpdatesOptions, useDeleteProjectUpdate } from "@multica/core/projects";
import { useAuthStore } from "@multica/core/auth";
import { ProjectUpdateComposer } from "./project-update-composer";
import { ProjectUpdateCard } from "./project-update-card";

interface ProjectUpdatesTabProps {
  wsId: string;
  projectId: string;
}

export function ProjectUpdatesTab({ wsId, projectId }: ProjectUpdatesTabProps) {
  const { data: updates = [], isLoading } = useQuery(projectUpdatesOptions(wsId, projectId));
  const deleteUpdate = useDeleteProjectUpdate(wsId, projectId);
  const currentUserId = useAuthStore((s) => s.user?.id);
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4 p-4">
      <ProjectUpdateComposer wsId={wsId} projectId={projectId} />
      {isLoading ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading…</p>
      ) : updates.length === 0 ? (
        <p className="py-8 text-center text-sm text-muted-foreground">
          No updates yet. Post the first project update above.
        </p>
      ) : (
        updates.map((u) => (
          <ProjectUpdateCard
            key={u.id}
            update={u}
            // An author can remove their own update.
            canModerate={
              !!currentUserId && u.author_type === "member" && u.author_id === currentUserId
            }
            onDelete={(id) => deleteUpdate.mutate(id)}
          />
        ))
      )}
    </div>
  );
}
