import { View } from "react-native";
import { router } from "expo-router";
import ProjectsPage from "../more/projects";
import { Header } from "@/components/ui/header";
import { IconButton } from "@/components/ui/icon-button";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function ProjectsTab() {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  return (
    <View className="flex-1 bg-background">
      <Header
        title="Projects"
        right={
          <IconButton
            name="add"
            onPress={() => slug && router.push(`/${slug}/project/new`)}
            accessibilityLabel="New project"
          />
        }
      />
      <View className="flex-1">
        <ProjectsPage />
      </View>
    </View>
  );
}
