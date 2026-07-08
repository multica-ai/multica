/**
 * Label picker route for an existing issue — multi-select with inline
 * create. Uses native iOS Stack header + UISearchController via
 * `useNativeSearchBar` (sheet stays open across toggles; the user
 * dismisses via the sheet grabber or the Back button).
 */
import { useRef } from "react";
import { useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { LabelPickerBody } from "@/components/issue/pickers/label-picker-body";
import { issueDetailOptions } from "@/data/queries/issues";
import {
  useAttachLabel,
  useDetachLabel,
} from "@/data/mutations/issues";
import { useCreateLabel } from "@/data/mutations/labels";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function IssueLabelPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const attachLabel = useAttachLabel(id);
  const detachLabel = useDetachLabel(id);
  const createLabel = useCreateLabel();
  const query = useNativeSearchBar("Search labels", { autoFocus: true });

  // Synchronous lock to prevent double-submit on rapid taps on the Create
  // row before React state updates — mirrors web's `creatingRef` pattern in
  // `packages/views/issues/components/pickers/label-picker.tsx`.
  const creatingRef = useRef(false);

  const attached = issue?.labels ?? [];

  return (
    <LabelPickerBody
      attached={attached}
      query={query}
      onAttach={(label) => attachLabel.mutate({ label })}
      onDetach={(labelId) => detachLabel.mutate({ labelId })}
      onCreate={(name, color) => {
        if (creatingRef.current) return;
        creatingRef.current = true;
        createLabel.mutate(
          { name, color },
          {
            onSuccess: (label) => {
              attachLabel.mutate({ label });
            },
            onSettled: () => {
              creatingRef.current = false;
            },
          },
        );
      }}
    />
  );
}
