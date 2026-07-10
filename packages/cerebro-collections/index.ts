// Collections (FIR-1590 → Collections): per-folder access grants with a
// Settings → Collections surface and a per-folder "Valgt her / Arvet" editor.
// Headless surface only (types + query helpers + mutations); the UI lives under
// the `./views` subpath so non-UI consumers don't drag in React components.

export type {
  FolderGrant,
  GrantRole,
  GrantSurface,
  GranteeType,
  RemoveFolderGrantInput,
  UpsertFolderGrantInput,
} from "./types";
export type { GrantView } from "./api";
export { folderGrantKeys, folderGrantsOptions } from "./queries";
// FIR-2688: folder-list keys so a surface (Documents/Notes/Skills/Autopilots)
// that creates/moves/deletes a folder can invalidate the Collections tab's
// cache and keep it aligned. Surfaces already depend on this package.
export { collectionFolderKeys } from "./queries";
export { useRemoveFolderGrant, useUpsertFolderGrant } from "./mutations";
