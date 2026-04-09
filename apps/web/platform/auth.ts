import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { api } from "./api";
import { webStorage } from "./storage";
import {
  setLoggedInCookie,
  clearLoggedInCookie,
} from "../features/auth/auth-cookie";

export const useAuthStore = createAuthStore({
  api,
  storage: webStorage,
  onLogin: setLoggedInCookie,
  onLogout: clearLoggedInCookie,
});

registerAuthStore(useAuthStore);
