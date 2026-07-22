/**
 * Project picker route for the in-progress new-issue draft. Uses the same
 * native iOS Stack header + UISearchController pattern as
 * `issue/[id]/picker/project.tsx`.
 */
import { router } from "expo-router";
import { useTranslation } from "react-i18next";
import { ProjectPickerBody } from "@/components/issue/pickers/project-picker-body";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function NewIssueProjectPickerRoute() {
  const { t } = useTranslation("issues");
  const project = useNewIssueDraftStore((s) => s.project);
  const setProject = useNewIssueDraftStore((s) => s.setProject);
  const query = useNativeSearchBar(t("new_issue.picker.project.search_placeholder"), {
    autoFocus: true,
  });

  return (
    <ProjectPickerBody
      value={project}
      query={query}
      onChange={(next) => {
        setProject(next);
        router.back();
      }}
    />
  );
}
