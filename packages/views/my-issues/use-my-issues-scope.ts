"use client";

import { useStore } from "zustand";
import {
  myIssuesScopeFor,
  myIssuesViewStore,
  type MyIssuesScope,
} from "@multica/core/issues/stores/my-issues-view-store";
import { useNavigation } from "../navigation";

/** The scope the current My issues location shows. */
export function useMyIssuesScope(): MyIssuesScope {
  const { searchParams } = useNavigation();
  const stored = useStore(myIssuesViewStore, (s) => s.scope);
  return myIssuesScopeFor(searchParams, stored);
}
