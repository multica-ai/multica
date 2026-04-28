import { Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { useState } from "react";
import { useWorkspaceList } from "@multica/core/workspace/hooks";
import { setCurrentWorkspace } from "@multica/core/platform";
import type { Workspace } from "@multica/core/types";
import { Button } from "./primitives";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

export function WorkspaceHeader({
  centered = false,
  title,
}: {
  centered?: boolean;
  title?: string;
}) {
  const { workspace, setWorkspace } = useMobileWorkspace();
  const { data: workspaces = [] } = useWorkspaceList();
  const [open, setOpen] = useState(false);

  function selectWorkspace(next: Workspace) {
    setCurrentWorkspace(next.slug, next.id);
    setWorkspace(next);
    setOpen(false);
  }

  if (centered) {
    return (
      <View style={styles.centeredHeader}>
        <Pressable
          accessibilityRole="button"
          onPress={() => setOpen((value) => !value)}
          style={styles.centeredTitleButton}
        >
          <Text numberOfLines={1} style={styles.centeredWorkspace}>
            {workspace.name}
          </Text>
          <Text style={styles.chevron}>{open ? "⌃" : "⌄"}</Text>
        </Pressable>
        {open ? (
          <View style={styles.dropdown}>
            {workspaces.map((item) => (
              <Pressable
                key={item.id}
                onPress={() => selectWorkspace(item)}
                style={[
                  styles.dropdownRow,
                  item.id === workspace.id && styles.dropdownRowActive,
                ]}
              >
                <Text numberOfLines={1} style={styles.dropdownName}>
                  {item.name}
                </Text>
                <Text numberOfLines={1} style={styles.dropdownSlug}>
                  {item.slug}
                </Text>
              </Pressable>
            ))}
          </View>
        ) : null}
      </View>
    );
  }

  return (
    <View style={styles.header}>
      <View style={styles.titleGroup}>
        {title ? <Text style={styles.title}>{title}</Text> : null}
        <Pressable onPress={() => setOpen(true)}>
          <Text style={styles.workspace}>{workspace.name}</Text>
        </Pressable>
      </View>
      <Modal animationType="slide" onRequestClose={() => setOpen(false)} visible={open}>
        <View style={styles.modal}>
          <View style={styles.modalHeader}>
            <Text style={styles.title}>Workspaces</Text>
            <Button onPress={() => setOpen(false)} variant="ghost">
              Close
            </Button>
          </View>
          {workspaces.map((item) => (
            <Pressable
              key={item.id}
              onPress={() => selectWorkspace(item)}
              style={[
                styles.workspaceRow,
                item.id === workspace.id && styles.workspaceRowActive,
              ]}
            >
              <Text style={styles.workspaceName}>{item.name}</Text>
              <Text style={styles.workspaceSlug}>{item.slug}</Text>
            </Pressable>
          ))}
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    paddingBottom: spacing.md,
  },
  centeredHeader: {
    alignItems: "center",
    flex: 1,
    position: "relative",
    zIndex: 20,
  },
  centeredTitleButton: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.xs,
    maxWidth: "58%",
    minHeight: 44,
  },
  centeredWorkspace: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "600",
  },
  chevron: {
    color: colors.mutedForeground,
    fontSize: 16,
    fontWeight: "500",
  },
  dropdown: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    minWidth: 220,
    paddingVertical: spacing.xs,
    position: "absolute",
    top: 46,
    shadowColor: "#000000",
    shadowOffset: { height: 4, width: 0 },
    shadowOpacity: 0.14,
    shadowRadius: 10,
    elevation: 10,
  },
  dropdownRow: {
    gap: spacing.xs,
    minHeight: 50,
    justifyContent: "center",
    paddingHorizontal: spacing.md,
  },
  dropdownRowActive: {
    backgroundColor: colors.muted,
  },
  dropdownName: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
  },
  dropdownSlug: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  titleGroup: {
    gap: spacing.xs,
  },
  title: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "500",
  },
  workspace: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
  modal: {
    backgroundColor: colors.background,
    flex: 1,
    gap: spacing.sm,
    padding: spacing.lg,
    paddingTop: spacing.xl,
  },
  modalHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
    marginBottom: spacing.md,
  },
  workspaceRow: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.xs,
    padding: spacing.md,
  },
  workspaceRowActive: {
    backgroundColor: colors.muted,
  },
  workspaceName: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "500",
  },
  workspaceSlug: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
});
