/**
 * Project resources section. Read-mostly list of typed external pointers
 * (today: GitHub repos). Tap a row to open the URL in the system browser.
 * Long-press for delete (Pressable's onLongPress).
 *
 * Schema-tolerant by design — `resource_ref` is typed `unknown` in the
 * mobile schema (server may extend the shape per resource_type). We narrow
 * via `getRepoUrl()` only when the dispatch knows the type, so a future
 * resource_type renders as a generic row with the label instead of crashing.
 */
import { ActivityIndicator, Alert, Linking, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import type {
  GithubRepoResourceRef,
  ProjectResource,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { projectResourcesOptions } from "@/data/queries/projects";
import { useDeleteProjectResource } from "@/data/mutations/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  projectId: string;
  onAdd: () => void;
}

export function ProjectResourcesSection({ projectId, onAdd }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: resources, isLoading } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );
  const remove = useDeleteProjectResource(projectId);

  const onOpen = async (resource: ProjectResource) => {
    const url = getResourceUrl(resource);
    if (!url) return;
    const canOpen = await Linking.canOpenURL(url);
    if (canOpen) {
      await Linking.openURL(url);
    }
  };

  const onLongPress = (resource: ProjectResource) => {
    Alert.alert(
      "Detach resource?",
      describeResource(resource),
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Detach",
          style: "destructive",
          onPress: () => remove.mutate(resource.id),
        },
      ],
    );
  };

  return (
    <View>
      <View className="flex-row items-center justify-between px-4 py-2 bg-background">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Resources
        </Text>
        <Pressable onPress={onAdd} className="px-2 py-1 active:bg-secondary rounded">
          <Text className="text-xs text-brand">Add</Text>
        </Pressable>
      </View>
      {isLoading ? (
        <View className="px-4 py-4 items-center">
          <ActivityIndicator size="small" />
        </View>
      ) : !resources || resources.length === 0 ? (
        <View className="px-4 py-3">
          <Text className="text-sm text-muted-foreground/70">
            No resources attached.
          </Text>
        </View>
      ) : (
        resources.map((resource) => (
          <ResourceRow
            key={resource.id}
            resource={resource}
            onPress={() => onOpen(resource)}
            onLongPress={() => onLongPress(resource)}
          />
        ))
      )}
    </View>
  );
}

function ResourceRow({
  resource,
  onPress,
  onLongPress,
}: {
  resource: ProjectResource;
  onPress: () => void;
  onLongPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  return (
    <Pressable
      onPress={onPress}
      onLongPress={onLongPress}
      delayLongPress={400}
      className="flex-row items-center gap-3 px-4 py-2.5 active:bg-secondary border-t border-border"
    >
      <Ionicons
        name={iconFor(resource.resource_type)}
        size={16}
        color={THEME[colorScheme].mutedForeground}
      />
      <View className="flex-1">
        <Text className="text-sm text-foreground" numberOfLines={1}>
          {resource.label ?? describeResource(resource)}
        </Text>
        {resource.label ? (
          <Text className="text-xs text-muted-foreground" numberOfLines={1}>
            {describeResource(resource)}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

function iconFor(type: string): keyof typeof Ionicons.glyphMap {
  if (type === "github_repo") return "logo-github";
  return "link-outline";
}

function getResourceUrl(resource: ProjectResource): string | null {
  if (resource.resource_type === "github_repo") {
    const ref = resource.resource_ref as GithubRepoResourceRef | undefined;
    return ref?.url ?? null;
  }
  // Unknown type — try a `.url` field as a generic fallback.
  const ref = resource.resource_ref as { url?: unknown } | undefined;
  return typeof ref?.url === "string" ? ref.url : null;
}

function describeResource(resource: ProjectResource): string {
  return getResourceUrl(resource) ?? resource.resource_type;
}
