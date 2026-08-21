/**
 * 服务器列表页 —— 应用内切换 API 服务器地址(RUYI-4)。
 *
 * 路由刻意放在 (auth) / (app) 两个分组之外:未登录用户连自建后端是核心
 * 场景,登录前必须可达。
 *
 * 视觉沿用 `more/settings.tsx` 的 SectionGroup / 行模式,不引入新组件。
 * 内置项置顶且没有「…」菜单 —— 不可编辑删除直接体现为「没有可点的入口」,
 * 而不是置灰按钮让用户点一次才知道禁用。
 *
 * 行内「…」用 `components/ui/dropdown-menu`(@rn-primitives)而不是
 * `ActionSheetIOS`:后者的原生模块只在 iOS 侧实现,Android 上
 * `TurboModuleRegistry.get('ActionSheetManager')` 返回 null,组件内的
 * invariant 会直接抛 —— 编辑/删除是自定义服务器的唯一入口,在 Android
 * 上崩掉等于填错地址就只能卸载重装(QA 评审 P0-2)。
 */
import { useCallback } from "react";
import { Alert, Pressable, ScrollView, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Ionicons } from "@expo/vector-icons";
import { Stack, router } from "expo-router";
import { useQueryClient } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Separator } from "@/components/ui/separator";
import { IconButton } from "@/components/ui/icon-button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useServerStore } from "@/data/server-store";
import { pickActiveServer, type ServerEntry } from "@/data/server-config";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

export default function ServerListScreen() {
  const servers = useServerStore((s) => s.servers);
  const activeServerId = useServerStore((s) => s.activeServerId);
  const setActiveServer = useServerStore((s) => s.setActiveServer);
  const removeServer = useServerStore((s) => s.removeServer);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const clearWorkspace = useWorkspaceStore((s) => s.clear);
  const qc = useQueryClient();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const active = pickActiveServer(servers, activeServerId);

  /**
   * 真正执行切换。除清 token 外必须同时清 workspace-store 与 React Query
   * 缓存 —— 不同后端的工作区 slug 与数据互不相通,漏清会把 A 后端的缓存
   * 渲染在 B 后端会话里。
   */
  const doSwitch = useCallback(
    async (entry: ServerEntry) => {
      // 先切换并落盘,成功后才清本地会话 —— 落盘失败时用户留在原会话里,
      // 反过来就会把人踢出去却什么都没切成。logout 只清本地 token,
      // 不发网络请求,所以此刻地址已变也不影响。
      try {
        await setActiveServer(entry.id);
      } catch (err) {
        Alert.alert(
          "Switch failed",
          err instanceof Error ? err.message : "Could not switch servers.",
        );
        return;
      }
      if (!user) return;
      await clearWorkspace();
      await logout();
      qc.clear();
      router.replace("/login");
    },
    [user, clearWorkspace, logout, qc, setActiveServer],
  );

  const onSelect = useCallback(
    (entry: ServerEntry) => {
      if (entry.id === active.id) return;
      // 未登录时直接切换,不打扰;已登录必须二次确认(会退出账号)。
      if (!user) {
        void doSwitch(entry);
        return;
      }
      Alert.alert(
        "Switch server?",
        `You'll be signed out of the current account before connecting to ${entry.name || entry.apiUrl}.`,
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Switch",
            style: "destructive",
            onPress: () => void doSwitch(entry),
          },
        ],
      );
    },
    [active.id, user, doSwitch],
  );

  const onDelete = useCallback(
    (entry: ServerEntry) => {
      Alert.alert(
        "Delete server",
        `Remove ${entry.name || entry.apiUrl} from this device?`,
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Delete",
            style: "destructive",
            onPress: () => void removeServer(entry.id),
          },
        ],
      );
    },
    [removeServer],
  );

  return (
    <SafeAreaView className="flex-1 bg-background" edges={["bottom"]}>
      <Stack.Screen
        options={{
          title: "Servers",
          headerBackTitle: "Back",
          headerRight: () => (
            <IconButton
              name="add"
              onPress={() => router.push("/server-settings/new")}
              accessibilityLabel="Add server"
            />
          ),
        }}
      />
      <ScrollView
        className="flex-1"
        contentContainerClassName="px-4 py-4 gap-6"
      >
        <View className="gap-2">
          <Text className="text-xs uppercase tracking-wider text-muted-foreground px-1">
            Servers
          </Text>
          <View className="rounded-md border border-border bg-card overflow-hidden">
            {servers.map((entry, idx) => (
              <View key={entry.id}>
                <ServerRow
                  entry={entry}
                  isActive={entry.id === active.id}
                  iconColor={mutedFg}
                  onPress={() => onSelect(entry)}
                  onEdit={() => router.push(`/server-settings/${entry.id}`)}
                  onDelete={() => onDelete(entry)}
                />
                {idx < servers.length - 1 ? <Separator /> : null}
              </View>
            ))}
          </View>
          <Text className="text-xs text-muted-foreground px-1">
            Tap a server to connect to it. Switching signs you out of the
            current account.
          </Text>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}

/**
 * 「…」菜单刻意是行 Pressable 的**兄弟节点**而非子节点。
 *
 * 之前的结构把它嵌在设了 `disabled={isActive}` 的 Pressable 里,而 RN 的
 * `disabled` 会经 usePressability 吞掉整棵子树的手势响应 —— 当前生效的
 * 自定义服务器因此连编辑入口都点不动(QA 评审 P1)。拆成兄弟节点后,
 * 行的可点状态与菜单的可用性彻底解耦,`disabled` 也就不再需要:
 * `onSelect` 内部对「点的就是当前项」已经直接 return。
 */
function ServerRow({
  entry,
  isActive,
  iconColor,
  onPress,
  onEdit,
  onDelete,
}: {
  entry: ServerEntry;
  isActive: boolean;
  iconColor: string;
  onPress: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <View className="flex-row items-center pr-2">
      <Pressable
        onPress={onPress}
        className="flex-1 flex-row items-center px-4 py-3.5 active:bg-secondary gap-3"
      >
        <View className="flex-1">
          <View className="flex-row items-center gap-2">
            <Text className="text-base font-medium text-foreground">
              {entry.name || entry.apiUrl}
            </Text>
            {entry.builtIn ? (
              <View className="rounded bg-muted px-1.5 py-0.5">
                <Text className="text-xs text-muted-foreground">Built-in</Text>
              </View>
            ) : null}
          </View>
          <Text className="text-xs text-muted-foreground mt-0.5">
            {entry.apiUrl}
          </Text>
        </View>
        {isActive ? (
          <Ionicons name="checkmark" size={18} color={iconColor} />
        ) : null}
      </Pressable>
      {/* 内置项不可编辑不可删,直接不出菜单 —— 没有可点的入口比置灰更清楚。 */}
      {entry.builtIn ? null : (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <IconButton
              name="ellipsis-horizontal"
              iconSize={18}
              color={iconColor}
              accessibilityLabel={`Actions for ${entry.name || entry.apiUrl}`}
            />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            <DropdownMenuItem onPress={onEdit}>
              <Text>Edit</Text>
            </DropdownMenuItem>
            {/* 当前生效项不给删除 —— 删掉正在用的服务器会造成一次隐式的
                地址变化,行为不可预期。用户需要先切走再删。 */}
            {isActive ? null : (
              <DropdownMenuItem variant="destructive" onPress={onDelete}>
                <Text>Delete</Text>
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </View>
  );
}
