/**
 * HarmonyOS port of apps/mobile/components/project/project-resources-section.tsx.
 * Read-mostly list of typed external pointers (today: GitHub repos). Tap a
 * row to open the URL in the system browser. Long-press for delete
 * (Pressable's onLongPress).
 *
 * Schema-tolerant by design — `resource_ref` is typed `unknown` in the
 * mobile schema (server may extend the shape per resource_type). We narrow
 * via `getResourceUrl()` only when the dispatch knows the type, so a future
 * resource_type renders as a generic row with the label instead of crashing.
 *
 * Icon note: Ionicons has a `logo-github` glyph but it is not in the bundled
 * glyph list yet (components/ui/ionicon-glyphs.ts — see
 * scripts/generate-icons.mjs). <Icon> renders unknown names as nothing, so
 * the github rows fall back to the generic link glyph until the glyph ships.
 */
import { ActivityIndicator, Alert, Linking, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type {
  GithubRepoResourceRef,
  ProjectResource,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { IONICON_GLYPHS } from "@/components/ui/ionicon-glyphs";
import { projectResourcesOptions } from "@/data/queries/projects";
import { useDeleteProjectResource } from "@/data/mutations/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha } from "@/lib/theme";

interface Props {
  projectId: string;
  onAdd: () => void;
}

const GITHUB_ICON: keyof typeof IONICON_GLYPHS = IONICON_GLYPHS["logo-github"]
  ? "logo-github"
  : "link-outline";

export function ProjectResourcesSection({ projectId, onAdd }: Props) {
  const c = useThemeColors();
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
      <View style={styles.headerRow}>
        <Text
          style={[styles.headerTitle, { color: c.mutedForeground }]}
        >
          Resources
        </Text>
        <Pressable
          onPress={onAdd}
          style={({ pressed }) => [
            styles.addButton,
            pressed ? { backgroundColor: c.secondary } : null,
          ]}
        >
          <Text style={[styles.addLabel, { color: c.brand }]}>Add</Text>
        </Pressable>
      </View>
      {isLoading ? (
        <View style={styles.loading}>
          <ActivityIndicator size="small" />
        </View>
      ) : !resources || resources.length === 0 ? (
        <View style={styles.empty}>
          <Text style={{ fontSize: 14, color: withAlpha(c.mutedForeground, 0.7) }}>
            No resources attached.
          </Text>
        </View>
      ) : (
        resources.map((resource) => (
          <ResourceRow
            key={resource.id}
            resource={resource}
            onPress={() => void onOpen(resource)}
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
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onPress}
      onLongPress={onLongPress}
      delayLongPress={400}
      style={({ pressed }) => [
        // flex-row items-center gap-3 px-4 py-2.5 border-t border-border
        styles.row,
        { borderTopColor: c.border },
        pressed ? { backgroundColor: c.secondary } : null,
      ]}
    >
      <Icon name={iconFor(resource.resource_type)} size={16} color={c.mutedForeground} />
      <View style={styles.rowMain}>
        <Text style={[styles.rowTitle, { color: c.foreground }]} numberOfLines={1}>
          {resource.label ?? describeResource(resource)}
        </Text>
        {resource.label ? (
          <Text
            style={[styles.rowSubtitle, { color: c.mutedForeground }]}
            numberOfLines={1}
          >
            {describeResource(resource)}
          </Text>
        ) : null}
        {/* Its own line rather than appended to the URL: a custom label already
            takes the first slot, and this is the one project setting that
            silently changes what every task starts from. */}
        {checkoutRefOf(resource) ? (
          <View style={styles.refRow}>
            <Icon
              name="git-branch-outline"
              size={11}
              color={c.mutedForeground}
            />
            <Text
              style={[styles.rowSubtitle, { color: c.mutedForeground }]}
              numberOfLines={1}
            >
              {checkoutRefOf(resource)}
            </Text>
          </View>
        ) : null}
      </View>
    </Pressable>
  );
}

function iconFor(type: string): keyof typeof IONICON_GLYPHS {
  if (type === "github_repo") return GITHUB_ICON;
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

/**
 * The repo's pinned checkout ref, or null when tasks use the default branch.
 *
 * Matches the web/desktop badge in
 * packages/views/projects/components/project-resources-section.tsx — a ref set
 * on any client has to be visible on every client, or the two disagree about
 * what the project is configured to do.
 */
function checkoutRefOf(resource: ProjectResource): string | null {
  if (resource.resource_type !== "github_repo") return null;
  const ref = resource.resource_ref as GithubRepoResourceRef | undefined;
  const value = ref?.ref?.trim();
  return value ? value : null;
}

const styles = StyleSheet.create({
  // flex-row items-center justify-between px-4 py-2
  headerRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  // text-xs uppercase tracking-wider font-medium
  headerTitle: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
    fontWeight: "500",
  },
  // px-2 py-1 rounded-xs
  addButton: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 2 },
  addLabel: { fontSize: 12 },
  // px-4 py-4 items-center
  loading: { paddingHorizontal: 16, paddingVertical: 16, alignItems: "center" },
  // px-4 py-3
  empty: { paddingHorizontal: 16, paddingVertical: 12 },
  // flex-row items-center gap-3 px-4 py-2.5 border-t
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderTopWidth: 1,
  },
  rowMain: { flex: 1 },
  // text-sm
  rowTitle: { fontSize: 14 },
  // text-xs
  rowSubtitle: { fontSize: 12 },
  // flex-row items-center gap-1
  refRow: { flexDirection: "row", alignItems: "center", gap: 4 },
});
