/**
 * 服务器配置 store —— Zustand,沿用 `data/workspace-store.ts` 的既有模式。
 *
 * 这是 API/Web 地址的唯一事实源。原来三处在模块加载期读 `process.env` 的
 * 地方(api.ts / realtime-provider.tsx / attachment-url.ts)改为在调用时
 * 从这里同步读取,构建期环境变量降级为「内置默认项」的来源,不再被业务
 * 代码直接引用。
 *
 * 持久化用 AsyncStorage 而非 SecureStore:服务器地址不是机密,且
 * expo-secure-store 在 Android 上有 2KB 单值软上限,存列表有截断风险。
 *
 * 落盘规则:只存自定义项 + activeServerId。内置项每次启动从构建期 env
 * 合成,官方域名日后变更时升级 App 即自动跟上。
 */
import { create } from "zustand";
import AsyncStorage from "@react-native-async-storage/async-storage";
import {
  BUILT_IN_SERVER_ID,
  buildBuiltInServer,
  composeServerList,
  normalizeUrl,
  parsePersistedState,
  pickActiveServer,
  resolveWebUrl,
  serializePersistedState,
  type ServerEntry,
} from "./server-config";

const STORAGE_KEY = "multica_servers";

/** 内置项从构建期 env 合成一次。env 缺失时这里抛错 —— 与改造前 api.ts 的
 *  模块级检查失败面相同(构建配置错误在开发期即暴露)。 */
const BUILT_IN_SERVER = buildBuiltInServer(
  process.env.EXPO_PUBLIC_API_URL,
  process.env.EXPO_PUBLIC_WEB_URL,
);

export interface NewServerInput {
  name: string;
  apiUrl: string;
  webUrl: string | null;
}

interface ServerState {
  servers: ServerEntry[];
  activeServerId: string;
  /** hydrate 是否完成。首个 API 请求发出前必须为 true。 */
  hydrated: boolean;
  /** 冷启动读取持久化配置。`authStore.initialize()` 最前面 await 它。 */
  hydrate: () => Promise<void>;
  addServer: (input: NewServerInput) => Promise<ServerEntry>;
  updateServer: (id: string, input: NewServerInput) => Promise<void>;
  removeServer: (id: string) => Promise<void>;
  setActiveServer: (id: string) => Promise<void>;
}

function createServerId(): string {
  const rand = Math.random().toString(36).slice(2, 10);
  const ts = Date.now().toString(36);
  return `srv_${rand}${ts}`;
}

export const useServerStore = create<ServerState>((set, get) => {
  /**
   * 落盘 + 状态写入,所有变更方法的统一出口。
   *
   * 顺序是「先写盘再改内存」:写盘失败时内存状态保持不变、异常上抛给
   * UI 提示,不会出现「界面显示切过去了、重启又弹回来」的分裂状态。
   */
  const commit = async (servers: ServerEntry[], activeServerId: string) => {
    await AsyncStorage.setItem(
      STORAGE_KEY,
      serializePersistedState(servers, activeServerId),
    );
    set({ servers, activeServerId });
  };

  return {
    servers: composeServerList(BUILT_IN_SERVER, []),
    activeServerId: BUILT_IN_SERVER_ID,
    hydrated: false,

    hydrate: async () => {
      let raw: string | null = null;
      try {
        raw = await AsyncStorage.getItem(STORAGE_KEY);
      } catch {
        // 读失败(存储不可用)按「没有自定义配置」处理,行为与现版一致。
        raw = null;
      }
      const persisted = parsePersistedState(raw);
      const servers = composeServerList(
        BUILT_IN_SERVER,
        persisted?.servers ?? [],
      );
      // activeServerId 指向已不存在的项时回退内置项,避免死在无效地址上。
      const active = pickActiveServer(
        servers,
        persisted?.activeServerId ?? BUILT_IN_SERVER_ID,
      );
      set({ servers, activeServerId: active.id, hydrated: true });
    },

    addServer: async (input) => {
      const entry: ServerEntry = {
        id: createServerId(),
        name: input.name.trim(),
        apiUrl: normalizeUrl(input.apiUrl),
        webUrl: input.webUrl ? normalizeUrl(input.webUrl) : null,
        builtIn: false,
      };
      const { servers, activeServerId } = get();
      await commit([...servers, entry], activeServerId);
      return entry;
    },

    updateServer: async (id, input) => {
      const { servers, activeServerId } = get();
      const next = servers.map((s) =>
        s.id === id && !s.builtIn
          ? {
              ...s,
              name: input.name.trim(),
              apiUrl: normalizeUrl(input.apiUrl),
              webUrl: input.webUrl ? normalizeUrl(input.webUrl) : null,
            }
          : s,
      );
      await commit(next, activeServerId);
    },

    removeServer: async (id) => {
      const { servers, activeServerId } = get();
      // 当前生效项与内置项都不可删 —— UI 侧已经不给入口,这里兜底,
      // 免得未来新调用点绕过 UI 把 App 打到无服务器可用的状态。
      if (id === activeServerId || id === BUILT_IN_SERVER_ID) return;
      await commit(
        servers.filter((s) => s.id !== id),
        activeServerId,
      );
    },

    setActiveServer: async (id) => {
      const { servers } = get();
      if (!servers.some((s) => s.id === id)) return;
      await commit(servers, id);
    },
  };
});

/**
 * 同步读取当前生效项 —— 供非 React 代码用(镜像 getCurrentSlug 模式)。
 *
 * hydrate 未完成就走到这里属于编程错误(初始化顺序被改坏了),表现是
 * 短暂连到内置默认服务器上,很难从现象反推。正确性目前由
 * `authStore.initialize()` 里的 `await hydrate()` 时序保证,这里只做一道
 * 开发期断言 —— 否则 `hydrated` 就是个没有消费点的死字段,会让后来人
 * 误以为存在一道运行时守卫(QA 评审 P2)。
 */
export function getActiveServer(): ServerEntry {
  const { servers, activeServerId, hydrated } = useServerStore.getState();
  if (__DEV__ && !hydrated) {
    console.warn(
      "[server-store] getActiveServer() called before hydrate() finished — " +
        "the request will go to the built-in server. Check that " +
        "authStore.initialize() still awaits useServerStore.hydrate() first.",
    );
  }
  return pickActiveServer(servers, activeServerId);
}

/** API 基地址。api.ts / realtime / attachment-url 三处调用时读取。 */
export function getApiUrl(): string {
  return getActiveServer().apiUrl;
}

/** Web 基地址。恒有值(webUrl 为空时回退 apiUrl),三处跳转菜单恒显示。 */
export function getWebUrl(): string {
  return resolveWebUrl(getActiveServer());
}
