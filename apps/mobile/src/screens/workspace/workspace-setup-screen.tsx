import { useMemo, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@multica/core/auth";
import { completeOnboarding } from "@multica/core/onboarding";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import { useMobileLogout } from "../../auth/use-mobile-logout";
import { Button, Field, Heading, Screen } from "../../components/ui/primitives";
import { colors, radii, spacing } from "../../theme/tokens";

function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .replace(/-{2,}/g, "-");
}

export function WorkspaceSetupScreen() {
  const { t } = useTranslation();
  const user = useAuthStore((state) => state.user);
  const logout = useMobileLogout();
  const createWorkspace = useCreateWorkspace();
  const [name, setName] = useState(
    user?.name
      ? t("workspace_setup.user_workspace", { name: user.name })
      : t("workspace_setup.default_name"),
  );
  const [slug, setSlug] = useState(() =>
    slugify(user?.name ? `${user.name}'s Workspace` : "my-workspace"),
  );
  const [slugEdited, setSlugEdited] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const normalizedSlug = useMemo(() => slugify(slug), [slug]);
  const canSubmit =
    name.trim().length > 0 &&
    normalizedSlug.length > 0 &&
    !createWorkspace.isPending;

  function handleNameChange(value: string) {
    setName(value);
    if (!slugEdited) {
      setSlug(slugify(value));
    }
  }

  function handleSlugChange(value: string) {
    setSlugEdited(true);
    setSlug(slugify(value));
  }

  async function handleCreateWorkspace() {
    if (!canSubmit) return;
    setError(null);
    try {
      const workspace = await createWorkspace.mutateAsync({
        name: name.trim(),
        slug: normalizedSlug,
      });
      setCurrentWorkspace(workspace.slug, workspace.id);
      if (!user?.onboarded_at) {
        void completeOnboarding().catch(() => {});
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("workspace_setup.unable_to_create"));
    }
  }

  return (
    <Screen>
      <View style={styles.container}>
        <View style={styles.header}>
          <Heading>{t("workspace_setup.title")}</Heading>
          <Text style={styles.subtitle}>
            {t("workspace_setup.subtitle")}
          </Text>
        </View>

        <View style={styles.card}>
          <Text style={styles.label}>{t("workspace_setup.name_label")}</Text>
          <Field
            autoCapitalize="words"
            onChangeText={handleNameChange}
            placeholder={t("workspace_setup.name_placeholder")}
            value={name}
          />
          <Text style={styles.label}>{t("workspace_setup.slug_label")}</Text>
          <Field
            autoCapitalize="none"
            onChangeText={handleSlugChange}
            placeholder={t("workspace_setup.slug_placeholder")}
            value={slug}
          />
          {error ? <Text style={styles.error}>{error}</Text> : null}
          <Button disabled={!canSubmit} onPress={handleCreateWorkspace}>
            {createWorkspace.isPending
              ? t("workspace_setup.creating")
              : t("workspace_setup.create")}
          </Button>
        </View>

        <Button onPress={logout} variant="secondary">
          {t("common.log_out")}
        </Button>
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  container: {
    alignSelf: "center",
    flex: 1,
    gap: spacing.lg,
    justifyContent: "center",
    maxWidth: 520,
    width: "100%",
  },
  header: {
    gap: spacing.sm,
  },
  subtitle: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.md,
    padding: spacing.lg,
  },
  label: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "500",
  },
  error: {
    color: colors.destructive,
    fontSize: 14,
  },
});
