/**
 * Inbox notice detail — HarmonyOS port of
 * apps/mobile/app/(app)/[workspace]/inbox/[id].tsx, keeping this slice's
 * props interface (item, onBack) that app-shell.tsx wires.
 *
 * The iOS route read the id from the URL and re-read the RAW workspace-scoped
 * cache (deduplication can replace a row, but a sheet already opened for a
 * specific notification must remain stable); here the pushed item supplies
 * the identity (item.id + workspace check) and the same raw-cache lookup
 * decides what renders — an item that left the inbox shows the
 * "no longer available" state, exactly like iOS.
 *
 * The autopilot-quota branch resolves the billing recovery flow through the
 * shared core resolver + the ported billing queries. iOS opened the billing
 * URL through Linking with EXPO_PUBLIC_WEB_URL; the harmony bundle inlines
 * MULTICA_WEB_URL instead (AGENTS.md env section).
 *
 * Platform deltas (RNOH 0.82 — see AGENTS.md): NativeWind classes are
 * explicit StyleSheet styles (text-lg=18, px-4=16, py-3=12, py-8=32, gap-5=20,
 * text-base=16, leading-6=24, leading-5=20); the top safe-area inset comes
 * from @/lib/safe-area.
 */
import {
  ActivityIndicator,
  Linking,
  ScrollView,
  StyleSheet,
  View,
} from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { InboxItem } from "@multica/core/types";
import {
  resolveBillingRecovery,
  type BillingRecoveryKind,
} from "@multica/core/billing/recovery";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { SafeAreaView } from "@/lib/safe-area";
import { inboxListOptions } from "@/data/queries/inbox";
import {
  appConfigOptions,
  workspaceSubscriptionSummaryOptions,
} from "@/data/queries/billing";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import {
  getAutopilotQuotaBody,
  getInboxDisplayTitle,
} from "@/lib/inbox-display";

function BillingRecovery({
  recovery,
  billingUrl,
}: {
  recovery: BillingRecoveryKind;
  billingUrl: string | null;
}) {
  const c = useThemeColors();
  switch (recovery) {
    case "checking":
      return <ActivityIndicator />;
    case "billing_disabled":
      return (
        <Text style={recoveryStyles(c).recoveryNote}>
          Billing changes are unavailable for this workspace. Contact your
          workspace administrator for help.
        </Text>
      );
    case "contact_admin":
      return (
        <Text style={recoveryStyles(c).recoveryNote}>
          Ask a workspace owner or admin to review the billing options.
        </Text>
      );
    case "checkout":
    case "portal":
    case "billing":
    case "billing_unavailable":
      return billingUrl ? (
        <Button
          onPress={() => void Linking.openURL(billingUrl)}
          accessibilityLabel="Review billing options"
        >
          <Text>Review billing options</Text>
        </Button>
      ) : (
        <Text style={recoveryStyles(c).recoveryNote}>
          Open Multica on the web to review billing options.
        </Text>
      );
  }
}

export function InboxDetailScreen({
  item: pushedItem,
  onBack,
}: {
  item: InboxItem;
  onBack: () => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const wsId = useWorkspaceStore((st) => st.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((st) => st.currentWorkspaceSlug);
  const { data: items, isLoading } = useQuery(inboxListOptions(wsId));

  // Read the raw workspace-scoped cache: deduplication can replace a row,
  // but a sheet already opened for a specific notification must remain
  // stable. The pushed item is the identity, not the projection.
  const item = items?.find(
    (candidate) =>
      candidate.id === pushedItem.id && candidate.workspace_id === wsId,
  );
  const isQuotaNotice = item?.type === "autopilot_quota_exceeded";
  const configQuery = useQuery({
    ...appConfigOptions(),
    enabled: isQuotaNotice,
  });
  const billingEnabled =
    configQuery.data?.feature_flags?.[
      BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG
    ] === true;
  const summaryQuery = useQuery(
    workspaceSubscriptionSummaryOptions(wsId, isQuotaNotice && billingEnabled),
  );
  const checkingConfig =
    isQuotaNotice && !configQuery.data && configQuery.isFetching;
  const recovery = resolveBillingRecovery({
    actions: summaryQuery.data?.availableActions,
    billingEnabled: checkingConfig || billingEnabled,
    loading:
      checkingConfig ||
      (billingEnabled &&
        !summaryQuery.data?.availableActions &&
        summaryQuery.isFetching),
  });
  const webUrl = process.env.MULTICA_WEB_URL?.replace(/\/+$/, "");
  const billingUrl =
    webUrl && wsSlug ? `${webUrl}/${wsSlug}/settings?tab=billing` : null;
  const body = item ? getAutopilotQuotaBody(item) : null;

  return (
    <SafeAreaView edges={["top"]} style={s.screen}>
      <View style={s.header}>
        <Text style={s.headerTitle} numberOfLines={1}>
          {item ? getInboxDisplayTitle(item) : "Notification"}
        </Text>
        <IconButton
          name="close"
          variant="secondary"
          style={s.closeButton}
          onPress={onBack}
          accessibilityLabel="Close notification"
        />
      </View>

      {isLoading ? (
        <View style={s.centered}>
          <ActivityIndicator />
        </View>
      ) : !item ||
        (item.type !== "autopilot_quota_exceeded" &&
          item.type !== "autopilot_paused") ? (
        <View style={s.unavailableWrap}>
          <Text style={s.unavailableText}>
            This notification is no longer available.
          </Text>
        </View>
      ) : (
        <ScrollView
          style={s.body}
          contentContainerStyle={s.bodyContent}
          showsVerticalScrollIndicator={false}
        >
          {body ? <Text style={s.bodyText}>{body}</Text> : null}

          {isQuotaNotice ? (
            <BillingRecovery recovery={recovery} billingUrl={billingUrl} />
          ) : null}
        </ScrollView>
      )}
    </SafeAreaView>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    // flex-1 bg-background
    screen: { flex: 1, backgroundColor: c.background },
    // flex-row items-center border-b border-border px-4 py-3
    header: {
      flexDirection: "row",
      alignItems: "center",
      borderBottomWidth: 1,
      borderBottomColor: c.border,
      paddingHorizontal: 16,
      paddingVertical: 12,
      gap: 8,
    },
    // flex-1 text-lg font-semibold text-foreground
    headerTitle: { flex: 1, fontSize: 18, fontWeight: "600", color: c.foreground },
    // size-7 rounded-full (overrides the IconButton's 40x40 icon size)
    closeButton: { width: 28, height: 28, borderRadius: 14 },
    // flex-1 items-center justify-center
    centered: { flex: 1, alignItems: "center", justifyContent: "center" },
    // px-4 py-8
    unavailableWrap: { paddingHorizontal: 16, paddingVertical: 32 },
    // text-sm text-muted-foreground text-center
    unavailableText: {
      fontSize: 14,
      color: c.mutedForeground,
      textAlign: "center",
    },
    // flex-1
    body: { flex: 1 },
    // gap-5 px-4 py-5
    bodyContent: { gap: 20, paddingHorizontal: 16, paddingVertical: 20 },
    // text-base leading-6 text-foreground
    bodyText: { fontSize: 16, lineHeight: 24, color: c.foreground },
  });

const recoveryStyles = (c: ThemeColors) =>
  StyleSheet.create({
    // text-sm leading-5 text-muted-foreground
    recoveryNote: {
      fontSize: 14,
      lineHeight: 20,
      color: c.mutedForeground,
    },
  });
