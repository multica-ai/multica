// FIR-1590: folder-level access control picker. Mirrors the per-note "Share"
// menu (Only you / Selected colleagues / Whole team) onto a folder row. Setting
// access on a folder locks the whole folder and everything inside it.
//
// Self-contained: fetches the workspace member list and the current user, and
// loads the share list lazily only when a "shared" folder's owner opens it.
// Gated by the cerebro_folder_access feature flag — renders nothing when off.
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock, Users, Globe } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuCheckboxItem,
  DropdownMenuGroup,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import type { ArtifactFolder, FolderVisibility } from "@multica/core/types";
import {
  artifactFolderSharesOptions,
  useSetArtifactFolderVisibility,
} from "../../core";
import {
  FOLDER_VISIBILITY_LABELS,
  folderVisibility,
} from "../../core/folder-access";

const VIS_ICON: Record<FolderVisibility, React.ReactNode> = {
  private: <Lock className="size-3" />,
  shared: <Users className="size-3" />,
  workspace: <Globe className="size-3" />,
};

export function FolderAccessControl({
  folder,
  className,
}: {
  folder: ArtifactFolder;
  className?: string;
}) {
  const enabled = useFeatureFlag("cerebro_folder_access");
  const wsId = useWorkspaceId();
  const myId = useAuthStore((s: { user: { id: string } | null }) => s.user?.id);
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: Boolean(enabled && wsId),
  });
  const setVisibility = useSetArtifactFolderVisibility();

  const visibility = folderVisibility(folder.visibility);
  // Legacy folders (no owner) can be claimed by any member; once owned, only
  // the owner may change access.
  const canEdit = !folder.owner_id || folder.owner_id === myId;

  const [open, setOpen] = React.useState(false);
  const [sharedIds, setSharedIds] = React.useState<string[]>([]);

  const { data: loadedShares } = useQuery(
    artifactFolderSharesOptions(wsId, folder.id, {
      enabled: open && canEdit && visibility === "shared",
    }),
  );
  React.useEffect(() => {
    if (loadedShares) setSharedIds(loadedShares);
  }, [loadedShares]);

  if (!enabled) return null;

  function applyVisibility(v: FolderVisibility) {
    setVisibility.mutate({
      id: folder.id,
      visibility: v,
      sharedUserIds: v === "shared" ? sharedIds : undefined,
    });
  }

  function toggleShare(userId: string) {
    const next = sharedIds.includes(userId)
      ? sharedIds.filter((id) => id !== userId)
      : [...sharedIds, userId];
    setSharedIds(next);
    setVisibility.mutate({
      id: folder.id,
      visibility: "shared",
      sharedUserIds: next,
    });
  }

  // Non-owners only see an indicator when the folder is actually restricted —
  // no clutter on the default "Whole team" folders.
  if (!canEdit) {
    if (visibility === "workspace") return null;
    return (
      <Badge
        variant="outline"
        className={cn("gap-1 px-1.5 py-0 text-[10px]", className)}
        title={FOLDER_VISIBILITY_LABELS[visibility]}
      >
        {VIS_ICON[visibility]}
      </Badge>
    );
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        render={
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="Folder access"
            title={`Access: ${FOLDER_VISIBILITY_LABELS[visibility]}`}
            className={cn(
              "shrink-0 text-muted-foreground",
              // Visible by default when restricted; hover-revealed when open to
              // everyone, so the default tree stays clean.
              visibility === "workspace" &&
                "opacity-0 focus:opacity-100 group-hover:opacity-100",
              className,
            )}
          >
            {VIS_ICON[visibility]}
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuRadioGroup
          value={visibility}
          onValueChange={(v) => applyVisibility(v as FolderVisibility)}
        >
          <DropdownMenuLabel>Who can see this folder</DropdownMenuLabel>
          <DropdownMenuRadioItem value="private">Only you</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="shared">
            Selected colleagues
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="workspace">
            Whole team
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        {visibility === "shared" && members.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>Share with</DropdownMenuLabel>
              {members
                .filter((m) => m.user_id !== myId)
                .map((m) => (
                  <DropdownMenuCheckboxItem
                    key={m.user_id}
                    checked={sharedIds.includes(m.user_id)}
                    onCheckedChange={() => toggleShare(m.user_id)}
                  >
                    {m.name}
                  </DropdownMenuCheckboxItem>
                ))}
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
