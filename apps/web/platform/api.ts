import { ApiClient } from "@multica/core/api/client";
import { setApiInstance } from "@multica/core/api";
import { createLogger } from "@multica/core/logger";

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "";

export const api = new ApiClient(API_BASE_URL, {
  logger: createLogger("api"),
  onUnauthorized: () => {
    if (typeof window !== "undefined") {
      localStorage.removeItem("multica_token");
      localStorage.removeItem("multica_workspace_id");
      if (window.location.pathname !== "/") {
        window.location.href = "/";
      }
    }
  },
});

// Register as the global singleton for @multica/core queries/mutations
setApiInstance(api);

// Hydrate from localStorage
if (typeof window !== "undefined") {
  const token = localStorage.getItem("multica_token");
  if (token) api.setToken(token);
  const wsId = localStorage.getItem("multica_workspace_id");
  if (wsId) api.setWorkspaceId(wsId);
}
