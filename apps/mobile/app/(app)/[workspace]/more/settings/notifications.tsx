/**
 * Inbox settings subscreen: where each kind of notification goes (Needs
 * action / Activity / Off) plus the system_notifications toggle, each backed
 * by an optimistic PATCH /api/notification-preferences.
 *
 * Copy and routing rules mirror packages/views/settings/components/
 * notifications-tab.tsx; both resolve routes through
 * @multica/core/notification-preferences/routes so the two never disagree.
 */
import { ActivityIndicator, Pressable, ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import type {
  NotificationPreferences,
  NotificationRoute,
} from "@multica/core/types";
import {
  NOTIFICATION_ROUTE_KINDS,
  notificationRoutesAreDefault,
  resolveNotificationRoute,
  withDefaultNotificationRoutes,
  withNotificationRoute,
} from "@multica/core/notification-preferences/routes";
import { Text } from "@/components/ui/text";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useWorkspaceStore } from "@/data/workspace-store";
import { notificationPreferenceOptions } from "@/data/queries/notification-preferences";
import { useUpdateNotificationPreferences } from "@/data/mutations/notification-preferences";
import { useT } from "@/lib/i18n";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

const ROUTES: readonly NotificationRoute[] = ["action", "activity", "muted"];

function RouteMark({ route, color }: { route: NotificationRoute; color: string }) {
  if (route === "action") {
    return (
      <View className="size-4 items-center justify-center">
        <View className="size-1.5 rounded-full bg-brand" />
      </View>
    );
  }
  return (
    <Ionicons
      name={route === "activity" ? "eye-outline" : "notifications-off-outline"}
      size={16}
      color={color}
    />
  );
}

export default function NotificationsSettingsScreen() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useT("settings");
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { data, isLoading, error } = useQuery(
    notificationPreferenceOptions(wsId),
  );
  const mutation = useUpdateNotificationPreferences();

  const preferences: NotificationPreferences = data?.preferences ?? {};
  const systemEnabled = preferences.system_notifications !== "muted";

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="flex-1 items-center justify-center bg-background px-6">
        <Text className="text-sm text-destructive text-center">
          {t("errors.load_failed")}
        </Text>
      </View>
    );
  }

  const routesAreDefault = notificationRoutesAreDefault(preferences);

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="px-4 py-4 gap-6"
    >
      <Section
        title={t("routing.title")}
        description={t("routing.description")}
        action={
          <Pressable
            accessibilityRole="button"
            disabled={routesAreDefault}
            onPress={() =>
              mutation.mutate(withDefaultNotificationRoutes(preferences))
            }
            className="flex-row items-center gap-1 active:opacity-60"
            style={{ opacity: routesAreDefault ? 0.4 : 1 }}
          >
            <Ionicons name="refresh" size={14} color={theme.mutedForeground} />
            <Text className="text-xs text-muted-foreground">
              {t("routing.reset")}
            </Text>
          </Pressable>
        }
      >
        {NOTIFICATION_ROUTE_KINDS.map((kind, idx) => {
          const route = resolveNotificationRoute(preferences, kind);
          const isLast = idx === NOTIFICATION_ROUTE_KINDS.length - 1;
          return (
            <View key={kind.key}>
              <View className="flex-row items-center px-4 py-3 gap-3">
                <View className="flex-1">
                  <Text className="text-base font-medium text-foreground">
                    {t(`routing.kinds.${kind.key}.label`)}
                  </Text>
                  <Text className="text-xs text-muted-foreground mt-0.5">
                    {t(`routing.kinds.${kind.key}.description`)}
                  </Text>
                </View>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Pressable
                      accessibilityRole="button"
                      accessibilityLabel={t(`routing.kinds.${kind.key}.label`)}
                      className="flex-row items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 active:bg-accent"
                    >
                      <RouteMark route={route} color={theme.mutedForeground} />
                      <Text
                        className={
                          route === "muted"
                            ? "text-sm text-muted-foreground"
                            : "text-sm text-foreground"
                        }
                      >
                        {t(`routing.routes.${route}.label`)}
                      </Text>
                      <Ionicons
                        name="chevron-down"
                        size={14}
                        color={theme.mutedForeground}
                      />
                    </Pressable>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-64 p-1">
                    {ROUTES.map((value) => (
                      <DropdownMenuItem
                        key={value}
                        className="items-start"
                        onPress={() => {
                          if (value !== route) {
                            mutation.mutate(
                              withNotificationRoute(preferences, kind, value),
                            );
                          }
                        }}
                      >
                        <View className="mt-0.5">
                          <RouteMark
                            route={value}
                            color={theme.mutedForeground}
                          />
                        </View>
                        <View className="flex-1">
                          <Text className="text-sm">
                            {t(`routing.routes.${value}.label`)}
                          </Text>
                          <Text className="text-xs text-muted-foreground">
                            {t(`routing.routes.${value}.description`)}
                          </Text>
                        </View>
                        {value === route ? (
                          <Ionicons
                            name="checkmark"
                            size={16}
                            color={theme.foreground}
                          />
                        ) : null}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </View>
              {!isLast ? <Separator /> : null}
            </View>
          );
        })}
      </Section>

      <Section title={t("groups.system")}>
        <View className="flex-row items-center px-4 py-3 gap-3">
          <View className="flex-1">
            <Text className="text-base font-medium text-foreground">
              {t("groups.system_notifications")}
            </Text>
            <Text className="text-xs text-muted-foreground mt-0.5">
              {t("groups.system_description")}
            </Text>
          </View>
          <Switch
            checked={systemEnabled}
            onCheckedChange={(checked) => {
              const next: NotificationPreferences = { ...preferences };
              if (checked) delete next.system_notifications;
              else next.system_notifications = "muted";
              mutation.mutate(next);
            }}
          />
        </View>
      </Section>
    </ScrollView>
  );
}

function Section({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-2">
      <View className="px-1">
        <View className="flex-row items-center justify-between gap-3">
          <Text className="text-xs uppercase tracking-wider text-muted-foreground">
            {title}
          </Text>
          {action}
        </View>
        {description ? (
          <Text className="text-xs text-muted-foreground mt-1">
            {description}
          </Text>
        ) : null}
      </View>
      <View className="rounded-md border border-border bg-card overflow-hidden">
        {children}
      </View>
    </View>
  );
}
