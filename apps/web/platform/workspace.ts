import { createWorkspaceStore, registerWorkspaceStore } from "@multica/core/workspace";
import { toast } from "sonner";
import { api } from "./api";
import { webStorage } from "./storage";

export const useWorkspaceStore = createWorkspaceStore(api, {
  storage: webStorage,
  onError: (msg) => toast.error(msg),
});

registerWorkspaceStore(useWorkspaceStore);
