/**
 * 服务器新增 / 编辑表单(RUYI-4)。`id === "new"` 走新增分支。
 *
 * 表单结构照搬 `more/settings/profile.tsx` 的 `ScrollView + gap-6` 骨架与
 * 「Text 标签 + TextField」两件套,不引入新组件。
 *
 * 主字段只有「名称 + 服务器地址」两项:绝大多数自建部署是单域名反代
 * (Web 与 API 同源),填一个地址就该通。Web 地址降级为默认收起的折叠
 * 高级项,留空即沿用服务器地址 —— 普通用户从头到尾看不到这四个字。
 */
import { useMemo, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQueryClient } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useServerStore } from "@/data/server-store";
import {
  SERVER_PROBE_PATH,
  findDuplicateServer,
  interpretProbeResponse,
  isPlainHttp,
  isValidServerUrl,
  normalizeUrl,
} from "@/data/server-config";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

/** 「测试连接」的超时。探活端点与判读规则见 server-config.ts。 */
const PROBE_TIMEOUT_MS = 8_000;

type ProbeState = "idle" | "probing" | "ok" | "failed";

export default function ServerFormScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const isNew = id === "new";

  const servers = useServerStore((s) => s.servers);
  const activeServerId = useServerStore((s) => s.activeServerId);
  const addServer = useServerStore((s) => s.addServer);
  const updateServer = useServerStore((s) => s.updateServer);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const clearWorkspace = useWorkspaceStore((s) => s.clear);
  const qc = useQueryClient();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const existing = useMemo(
    () => (isNew ? undefined : servers.find((s) => s.id === id)),
    [isNew, servers, id],
  );

  const [name, setName] = useState(existing?.name ?? "");
  const [apiUrl, setApiUrl] = useState(existing?.apiUrl ?? "");
  const [webUrl, setWebUrl] = useState(existing?.webUrl ?? "");
  // 用户显式填过 Web 地址就默认展开,不然他会以为自己的配置丢了。
  const [advancedOpen, setAdvancedOpen] = useState(!!existing?.webUrl);
  const [probe, setProbe] = useState<ProbeState>("idle");
  const [saving, setSaving] = useState(false);

  const apiTouched = apiUrl.trim().length > 0;
  const apiValid = isValidServerUrl(apiUrl);
  const duplicate = apiValid
    ? findDuplicateServer(servers, apiUrl, existing?.id)
    : undefined;
  const webTouched = webUrl.trim().length > 0;
  const webValid = !webTouched || isValidServerUrl(webUrl);

  const apiError = !apiTouched
    ? null
    : !apiValid
      ? "Enter a valid http(s) address"
      : duplicate
        ? "This address is already saved"
        : null;
  const webError = webTouched && !webValid ? "Enter a valid http(s) address" : null;
  const canSave = apiValid && !duplicate && webValid && !saving;

  const onProbe = async () => {
    if (!apiValid) return;
    setProbe("probing");
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS);
    try {
      const res = await fetch(`${normalizeUrl(apiUrl)}${SERVER_PROBE_PATH}`, {
        signal: controller.signal,
      });
      const reachable = interpretProbeResponse(
        res.status,
        res.headers.get("content-type"),
      );
      setProbe(reachable ? "ok" : "failed");
    } catch {
      setProbe("failed");
    } finally {
      clearTimeout(timer);
    }
  };

  /** 写入 store + 落盘。返回是否成功 —— 调用方据此决定要不要登出/导航。 */
  const persist = async (): Promise<boolean> => {
    setSaving(true);
    try {
      const input = {
        name,
        apiUrl,
        webUrl: webTouched ? webUrl : null,
      };
      if (isNew) await addServer(input);
      else if (existing) await updateServer(existing.id, input);
      return true;
    } catch (err) {
      Alert.alert(
        "Save failed",
        err instanceof Error ? err.message : "Could not save the server.",
      );
      return false;
    } finally {
      setSaving(false);
    }
  };

  const onSave = () => {
    if (!canSave) return;
    // 改的是当前生效项的地址 → 等同一次切换,走同一套登出确认。
    const switchesActiveServer =
      !!existing &&
      existing.id === activeServerId &&
      normalizeUrl(apiUrl) !== existing.apiUrl;

    const proceed = async () => {
      // 先落盘再登出:落盘失败(存储不可用)时用户还留在原会话里,
      // 反过来就会把人踢出去却没保存成任何东西。
      const saved = await persist();
      if (!saved) return;
      if (switchesActiveServer && user) {
        await clearWorkspace();
        await logout();
        qc.clear();
        router.replace("/login");
        return;
      }
      router.back();
    };

    if (switchesActiveServer && user) {
      Alert.alert(
        "Change server address?",
        "You'll be signed out of the current account before connecting to the new address.",
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Save",
            style: "destructive",
            onPress: () => void proceed(),
          },
        ],
      );
      return;
    }
    void proceed();
  };

  // 编辑一个已被删掉的条目(比如从列表返回后又被别处删除)—— 不做半残
  // 表单,直接告诉用户找不到。
  if (!isNew && !existing) {
    return (
      <View className="flex-1 items-center justify-center bg-background px-6">
        <Stack.Screen options={{ title: "Server" }} />
        <Text className="text-sm text-muted-foreground text-center">
          This server is no longer saved on this device.
        </Text>
      </View>
    );
  }

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="px-4 py-6 gap-6"
      keyboardShouldPersistTaps="handled"
    >
      <Stack.Screen
        options={{
          title: isNew ? "Add server" : "Edit server",
          headerBackTitle: "Back",
        }}
      />

      <View className="gap-4">
        <View>
          <Text className="text-xs text-muted-foreground mb-1.5">
            Name (optional)
          </Text>
          <TextField
            value={name}
            onChangeText={setName}
            placeholder="e.g. Home lab"
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="next"
          />
        </View>

        <View>
          <Text className="text-xs text-muted-foreground mb-1.5">
            Server address
          </Text>
          <TextField
            value={apiUrl}
            onChangeText={(v) => {
              setApiUrl(v);
              setProbe("idle");
            }}
            placeholder="https://api.example.com"
            keyboardType="url"
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="done"
            invalid={!!apiError}
          />
          {apiError ? (
            <Text className="text-xs text-destructive mt-1.5">{apiError}</Text>
          ) : isPlainHttp(apiUrl) ? (
            // 弱警告,不阻断保存 —— 自建内网用 http 是正常场景。
            <Text className="text-xs text-muted-foreground mt-1.5">
              This address uses plain http. Traffic won&apos;t be encrypted.
            </Text>
          ) : null}
        </View>

        <View className="gap-2">
          <Pressable
            onPress={() => setAdvancedOpen((v) => !v)}
            className="flex-row items-center gap-2 py-1 active:opacity-60"
          >
            <Ionicons
              name={advancedOpen ? "chevron-down" : "chevron-forward"}
              size={14}
              color={mutedFg}
            />
            <Text className="text-sm text-muted-foreground">
              Advanced: web address
            </Text>
          </Pressable>
          {advancedOpen ? (
            <View>
              <Text className="text-xs text-muted-foreground mb-1.5">
                Only needed when the web app is on a different domain than the
                server. Leave empty to reuse the server address.
              </Text>
              <TextField
                value={webUrl}
                onChangeText={setWebUrl}
                placeholder="Leave empty to reuse the server address"
                keyboardType="url"
                autoCapitalize="none"
                autoCorrect={false}
                returnKeyType="done"
                invalid={!!webError}
              />
              {webError ? (
                <Text className="text-xs text-destructive mt-1.5">
                  {webError}
                </Text>
              ) : null}
            </View>
          ) : null}
        </View>
      </View>

      <View className="gap-2">
        <Button variant="outline" onPress={onProbe} disabled={!apiValid}>
          <View className="flex-row items-center gap-2">
            {probe === "probing" ? <ActivityIndicator size="small" /> : null}
            <Text>{probe === "probing" ? "Testing…" : "Test connection"}</Text>
          </View>
        </Button>
        {probe === "ok" ? (
          <View className="flex-row items-center gap-1.5">
            <Ionicons name="checkmark-circle" size={14} color="#16a34a" />
            <Text className="text-xs" style={{ color: "#16a34a" }}>
              Connected
            </Text>
          </View>
        ) : probe === "failed" ? (
          <Text className="text-xs text-destructive">
            Couldn&apos;t reach this address. You can still save it.
          </Text>
        ) : null}
      </View>

      <Button onPress={onSave} disabled={!canSave}>
        <Text>{saving ? "Saving…" : "Save"}</Text>
      </Button>
    </ScrollView>
  );
}
