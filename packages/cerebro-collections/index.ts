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
export { useRemoveFolderGrant, useUpsertFolderGrant } from "./mutations";
