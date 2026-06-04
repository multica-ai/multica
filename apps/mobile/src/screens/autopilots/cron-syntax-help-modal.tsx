import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { useTranslation } from "react-i18next";
import { colors, radii, spacing } from "../../theme/tokens";

export function CronSyntaxHelpModal({
  onClose,
  visible,
}: {
  onClose: () => void;
  visible: boolean;
}) {
  const { t } = useTranslation();

  return (
    <Modal animationType="fade" onRequestClose={onClose} transparent visible={visible}>
      <Pressable
        accessibilityLabel={t("autopilots.cron_help_close")}
        accessibilityRole="button"
        onPress={onClose}
        style={styles.backdrop}
      >
        <Pressable onPress={(event) => event.stopPropagation()} style={styles.dialog}>
          <ScrollView contentContainerStyle={styles.content}>
            <Text style={styles.title}>{t("autopilots.cron_help_title")}</Text>
            <Text style={styles.bodyText}>{t("autopilots.cron_help_definition")}</Text>
            <View style={styles.fieldList}>
              <Text style={styles.fieldLine}>{t("autopilots.cron_help_minute")}</Text>
              <Text style={styles.fieldLine}>{t("autopilots.cron_help_hour")}</Text>
              <Text style={styles.fieldLine}>{t("autopilots.cron_help_day_of_month")}</Text>
              <Text style={styles.fieldLine}>{t("autopilots.cron_help_month")}</Text>
              <Text style={styles.fieldLine}>{t("autopilots.cron_help_day_of_week")}</Text>
            </View>
            <Text style={styles.bodyText}>{t("autopilots.cron_help_symbols")}</Text>
            <View style={styles.exampleBox}>
              <Text style={styles.exampleCode}>*/15 9,14 * * 1-5</Text>
              <Text style={styles.exampleText}>{t("autopilots.cron_help_example")}</Text>
            </View>
          </ScrollView>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: {
    alignItems: "center",
    backgroundColor: "rgba(24,24,27,0.38)",
    flex: 1,
    justifyContent: "center",
    padding: spacing.xl,
  },
  dialog: {
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    maxHeight: "78%",
    maxWidth: 360,
    overflow: "hidden",
    width: "100%",
  },
  content: {
    gap: spacing.md,
    padding: spacing.lg,
  },
  title: {
    color: colors.foreground,
    fontSize: 17,
    fontWeight: "700",
  },
  bodyText: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 20,
  },
  fieldList: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.xs,
    padding: spacing.md,
  },
  fieldLine: {
    color: colors.foreground,
    fontSize: 13,
    lineHeight: 18,
  },
  exampleBox: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    gap: spacing.xs,
    padding: spacing.md,
  },
  exampleCode: {
    color: colors.foreground,
    fontFamily: "Menlo",
    fontSize: 14,
    fontWeight: "600",
  },
  exampleText: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 18,
  },
});
