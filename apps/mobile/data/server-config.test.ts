/**
 * server-config 的纯逻辑测试(RUYI-4)。
 *
 * 覆盖目标是方案里点名的三类分支:
 *   1. 默认项合成 —— 内置项来源于构建期 env、不落盘、恒在列表首位
 *   2. webUrl 回退 —— null = 沿用 apiUrl,回退值绝不写进持久化载荷
 *   3. 切换后六处消费点取值 —— 三处 API(fetch / WS / 附件)+ 三处 Web 跳转
 *      全部只经由 pickActiveServer + resolveWebUrl 取值,这里按这两个函数
 *      的组合把六处的取值形态逐一钉住
 *
 * store 本身(Zustand + AsyncStorage)不在 vitest 的 node 可测范围内
 * (见 vitest.config.ts 的说明),故所有分支逻辑都下沉到本模块。
 */
import { describe, expect, it } from "vitest";
import {
  BUILT_IN_SERVER_ID,
  SERVER_PROBE_PATH,
  buildBuiltInServer,
  composeServerList,
  findDuplicateServer,
  interpretProbeResponse,
  isPlainHttp,
  isValidServerUrl,
  normalizeUrl,
  parsePersistedState,
  pickActiveServer,
  resolveWebUrl,
  serializePersistedState,
  SERVER_STORE_VERSION,
  toWebSocketUrl,
  type ServerEntry,
} from "./server-config";

const custom = (over: Partial<ServerEntry> = {}): ServerEntry => ({
  id: "srv_1",
  name: "Home lab",
  apiUrl: "https://hp-server.example.test",
  webUrl: null,
  builtIn: false,
  ...over,
});

describe("normalizeUrl", () => {
  it("去掉尾部斜杠与首尾空白", () => {
    expect(normalizeUrl("  https://api.example.test/  ")).toBe(
      "https://api.example.test",
    );
  });

  it("去掉多个连续尾斜杠", () => {
    expect(normalizeUrl("https://api.example.test///")).toBe(
      "https://api.example.test",
    );
  });

  it("保留路径前缀里的中间斜杠", () => {
    expect(normalizeUrl("https://example.test/multica/")).toBe(
      "https://example.test/multica",
    );
  });
});

describe("isValidServerUrl", () => {
  it("接受 https / http", () => {
    expect(isValidServerUrl("https://api.example.test")).toBe(true);
    expect(isValidServerUrl("http://192.168.1.10:8080")).toBe(true);
  });

  it("接受带尾斜杠与空白的输入(先规范化)", () => {
    expect(isValidServerUrl(" https://api.example.test/ ")).toBe(true);
  });

  it("拒绝空值与纯空白", () => {
    expect(isValidServerUrl("")).toBe(false);
    expect(isValidServerUrl("   ")).toBe(false);
  });

  it("拒绝缺协议的裸域名", () => {
    expect(isValidServerUrl("api.example.test")).toBe(false);
  });

  it("拒绝非 http(s) 协议", () => {
    expect(isValidServerUrl("ws://api.example.test")).toBe(false);
    expect(isValidServerUrl("ftp://api.example.test")).toBe(false);
    expect(isValidServerUrl("javascript:alert(1)")).toBe(false);
  });

  it("拒绝没有 host 的地址", () => {
    expect(isValidServerUrl("http://")).toBe(false);
    expect(isValidServerUrl("https://")).toBe(false);
    expect(isValidServerUrl("://nohost")).toBe(false);
  });

  /**
   * 这一组是校验从 URL 构造器换成正则的原因(QA 评审 P0-3):RN 的全局
   * URL 是 Libraries/Blob/URL.js 的 polyfill,单参构造从不抛异常,含空白
   * 的地址在真机上会被放行,而 vitest 的 node 环境用标准 WHATWG URL 判
   * 非法 —— 测试通过不代表真机正确。正则在两个环境行为一致。
   */
  it("拒绝内部含空白的地址(移动端粘贴常见)", () => {
    expect(isValidServerUrl("https:// spaces.example.test")).toBe(false);
    expect(isValidServerUrl("https://spaces .example.test")).toBe(false);
    expect(isValidServerUrl("https://a.test /api")).toBe(false);
    expect(isValidServerUrl("https://a.test\tb")).toBe(false);
  });

  it("拒绝 userinfo 混淆的地址", () => {
    expect(isValidServerUrl("https://evil.test@real.test")).toBe(false);
  });

  it("拒绝带 query / fragment 的地址(基地址不该有)", () => {
    expect(isValidServerUrl("https://a.test?x=1")).toBe(false);
    expect(isValidServerUrl("https://a.test#frag")).toBe(false);
  });

  it("接受带端口与路径前缀的地址(反代挂子路径的部署)", () => {
    expect(isValidServerUrl("https://a.test:8443/multica")).toBe(true);
  });

  it("file:// 等其他协议一律拒绝", () => {
    expect(isValidServerUrl("file:///etc/passwd")).toBe(false);
  });
});

describe("isPlainHttp", () => {
  it("明文 http 命中", () => {
    expect(isPlainHttp("http://192.168.1.10:8080")).toBe(true);
  });

  it("https 不命中", () => {
    expect(isPlainHttp("https://api.example.test")).toBe(false);
  });

  it("非法地址不命中(交给 isValidServerUrl 报错,不重复弹警告)", () => {
    expect(isPlainHttp("nonsense")).toBe(false);
    expect(isPlainHttp("")).toBe(false);
  });
});

describe("findDuplicateServer", () => {
  const servers = [custom({ id: "a", apiUrl: "https://a.example.test" })];

  it("命中同一地址", () => {
    expect(findDuplicateServer(servers, "https://a.example.test")?.id).toBe("a");
  });

  it("尾斜杠差异视为同一地址", () => {
    expect(findDuplicateServer(servers, "https://a.example.test/")?.id).toBe(
      "a",
    );
  });

  it("编辑自身时不算重复", () => {
    expect(
      findDuplicateServer(servers, "https://a.example.test", "a"),
    ).toBeUndefined();
  });

  it("不同地址不命中", () => {
    expect(
      findDuplicateServer(servers, "https://b.example.test"),
    ).toBeUndefined();
  });
});

describe("buildBuiltInServer(默认项合成)", () => {
  it("从构建期 env 合成内置项,并规范化地址", () => {
    const entry = buildBuiltInServer(
      "https://api.multica.ai/",
      "https://multica.ai/",
    );
    expect(entry).toEqual({
      id: BUILT_IN_SERVER_ID,
      name: "Multica Official",
      apiUrl: "https://api.multica.ai",
      webUrl: "https://multica.ai",
      builtIn: true,
    });
  });

  it("EXPO_PUBLIC_WEB_URL 缺失时 webUrl 为 null(后续回退 apiUrl)", () => {
    const entry = buildBuiltInServer("https://api.multica.ai", undefined);
    expect(entry.webUrl).toBeNull();
    expect(resolveWebUrl(entry)).toBe("https://api.multica.ai");
  });

  it("EXPO_PUBLIC_API_URL 缺失时抛错(构建配置错误在开发期即暴露)", () => {
    expect(() => buildBuiltInServer(undefined, undefined)).toThrow(
      /EXPO_PUBLIC_API_URL/,
    );
  });
});

describe("composeServerList", () => {
  const builtIn = buildBuiltInServer("https://api.multica.ai", "https://multica.ai");

  it("内置项恒在首位,自定义项按加入顺序跟随", () => {
    const list = composeServerList(builtIn, [
      custom({ id: "a" }),
      custom({ id: "b" }),
    ]);
    expect(list.map((s) => s.id)).toEqual([BUILT_IN_SERVER_ID, "a", "b"]);
  });

  it("过滤掉自定义列表里混入的 builtIn 项(避免出现两个内置项)", () => {
    const list = composeServerList(builtIn, [
      { ...custom({ id: "rogue" }), builtIn: true },
    ]);
    expect(list).toHaveLength(1);
    expect(list[0].id).toBe(BUILT_IN_SERVER_ID);
  });
});

describe("resolveWebUrl(webUrl 回退)", () => {
  it("webUrl 为 null 时回退 apiUrl —— 单域名部署零配置", () => {
    expect(resolveWebUrl(custom({ webUrl: null }))).toBe(
      "https://hp-server.example.test",
    );
  });

  it("webUrl 有值时用填写的值 —— 官方生产这类分域名部署", () => {
    expect(
      resolveWebUrl(
        custom({
          apiUrl: "https://api.multica.ai",
          webUrl: "https://multica.ai",
        }),
      ),
    ).toBe("https://multica.ai");
  });

  it("改了 apiUrl 后回退值自动跟着走(回退值没被复制钉死)", () => {
    const before = custom({ apiUrl: "https://old.example.test", webUrl: null });
    const after = { ...before, apiUrl: "https://new.example.test" };
    expect(resolveWebUrl(after)).toBe("https://new.example.test");
  });
});

describe("pickActiveServer", () => {
  const builtIn = buildBuiltInServer("https://api.multica.ai", "https://multica.ai");
  const servers = composeServerList(builtIn, [custom({ id: "srv_1" })]);

  it("按 id 选中当前项", () => {
    expect(pickActiveServer(servers, "srv_1").id).toBe("srv_1");
  });

  it("id 指向不存在的项时回退内置项(不死在无效地址上)", () => {
    expect(pickActiveServer(servers, "srv_gone").id).toBe(BUILT_IN_SERVER_ID);
  });
});

describe("持久化载荷", () => {
  const builtIn = buildBuiltInServer("https://api.multica.ai", "https://multica.ai");

  it("落盘时剔除内置项,只存自定义项 + activeServerId", () => {
    const servers = composeServerList(builtIn, [custom({ id: "srv_1" })]);
    const parsed = JSON.parse(serializePersistedState(servers, "srv_1"));
    expect(parsed.version).toBe(SERVER_STORE_VERSION);
    expect(parsed.servers.map((s: ServerEntry) => s.id)).toEqual(["srv_1"]);
    expect(parsed.activeServerId).toBe("srv_1");
  });

  it("webUrl 的回退值不落盘(只存用户显式填写的值)", () => {
    const servers = composeServerList(builtIn, [custom({ webUrl: null })]);
    const parsed = JSON.parse(serializePersistedState(servers, "srv_1"));
    expect(parsed.servers[0].webUrl).toBeNull();
  });

  it("往返一致:序列化后解析回来自定义项不变", () => {
    const entry = custom({ webUrl: "https://web.example.test" });
    const raw = serializePersistedState(
      composeServerList(builtIn, [entry]),
      entry.id,
    );
    expect(parsePersistedState(raw)).toEqual({
      version: SERVER_STORE_VERSION,
      servers: [entry],
      activeServerId: entry.id,
    });
  });

  it("空 / 非法 JSON → null(调用方回退到只有内置项的初始状态)", () => {
    expect(parsePersistedState(null)).toBeNull();
    expect(parsePersistedState("")).toBeNull();
    expect(parsePersistedState("{not json")).toBeNull();
    expect(parsePersistedState("[]")).toBeNull();
  });

  it("版本号不认 → null(留给未来的迁移逻辑)", () => {
    expect(
      parsePersistedState(
        JSON.stringify({ version: 99, servers: [], activeServerId: "default" }),
      ),
    ).toBeNull();
  });

  it("缺字段 → null", () => {
    expect(
      parsePersistedState(
        JSON.stringify({ version: SERVER_STORE_VERSION, servers: [] }),
      ),
    ).toBeNull();
    expect(
      parsePersistedState(
        JSON.stringify({
          version: SERVER_STORE_VERSION,
          activeServerId: "default",
        }),
      ),
    ).toBeNull();
  });

  it("单项损坏只丢那一项,其余服务器照常可用", () => {
    const raw = JSON.stringify({
      version: SERVER_STORE_VERSION,
      servers: [
        { id: "bad", apiUrl: "not-a-url", webUrl: null, builtIn: false },
        custom({ id: "good", apiUrl: "https://good.example.test" }),
      ],
      activeServerId: "good",
    });
    const parsed = parsePersistedState(raw);
    expect(parsed?.servers.map((s) => s.id)).toEqual(["good"]);
  });

  it("持久化数据里混进的内置项被忽略(内置项只从 env 合成)", () => {
    const raw = JSON.stringify({
      version: SERVER_STORE_VERSION,
      servers: [
        {
          id: BUILT_IN_SERVER_ID,
          name: "stale official",
          apiUrl: "https://old-api.multica.ai",
          webUrl: null,
          builtIn: true,
        },
      ],
      activeServerId: BUILT_IN_SERVER_ID,
    });
    expect(parsePersistedState(raw)?.servers).toEqual([]);
  });

  it("解析时规范化地址,非法 webUrl 降级为 null(而不是废掉整项)", () => {
    const raw = JSON.stringify({
      version: SERVER_STORE_VERSION,
      servers: [
        {
          id: "srv_1",
          name: "x",
          apiUrl: "https://a.example.test/",
          webUrl: "garbage",
          builtIn: false,
        },
      ],
      activeServerId: "srv_1",
    });
    const parsed = parsePersistedState(raw);
    expect(parsed?.servers[0].apiUrl).toBe("https://a.example.test");
    expect(parsed?.servers[0].webUrl).toBeNull();
  });
});

/**
 * 探活判读(QA 评审 P0-1 / P2)。参照对 owner 真实自建后端
 * hp-server.dzo-mermaid.ts.net 的实测:单域名反代下 `/health` 返回 404
 * text/html(落到 Web 前端的 404 页),`/api/me` 返回 401 —— 后者才是
 * 「server 活着」的可靠信号。
 */
describe("interpretProbeResponse", () => {
  it("401 / 403 判为可达 —— 探活不带 token,这正是 API 活着的信号", () => {
    expect(interpretProbeResponse(401, "text/plain; charset=utf-8")).toBe(true);
    expect(interpretProbeResponse(403, "application/json")).toBe(true);
  });

  it("2xx 且非 HTML 判为可达", () => {
    expect(interpretProbeResponse(200, "application/json")).toBe(true);
    expect(interpretProbeResponse(204, null)).toBe(true);
  });

  it("2xx 但返回 HTML 判为不可达 —— 请求落到了 Web 前端,没转到 server", () => {
    expect(interpretProbeResponse(200, "text/html; charset=utf-8")).toBe(false);
  });

  it("404 判为不可达", () => {
    expect(interpretProbeResponse(404, "text/html; charset=utf-8")).toBe(false);
    expect(interpretProbeResponse(404, "text/plain")).toBe(false);
  });

  it("5xx 判为不可达", () => {
    expect(interpretProbeResponse(502, "text/html")).toBe(false);
  });
});

describe("SERVER_PROBE_PATH", () => {
  it("是 /api/me —— /health 挂在根路由上,单域名部署下不可达", () => {
    expect(SERVER_PROBE_PATH).toBe("/api/me");
  });
});

describe("toWebSocketUrl", () => {
  it("https → wss", () => {
    expect(toWebSocketUrl("https://api.example.test")).toBe(
      "wss://api.example.test/ws",
    );
  });

  it("http → ws", () => {
    expect(toWebSocketUrl("http://192.168.1.10:8080")).toBe(
      "ws://192.168.1.10:8080/ws",
    );
  });

  it("先规范化尾斜杠,避免拼出 //ws", () => {
    expect(toWebSocketUrl("https://api.example.test/")).toBe(
      "wss://api.example.test/ws",
    );
  });
});

describe("切换服务器后六处消费点的取值", () => {
  // 三处 API 消费点(data/api.ts 的 fetch 拼接、realtime WS、附件 URL)
  // 与三处 Web 跳转点(issue / project / 评论菜单)全部只经由
  // pickActiveServer → apiUrl / resolveWebUrl 取值。这里按切换前后各跑
  // 一遍,把六处的取值形态一次钉死。
  const builtIn = buildBuiltInServer(
    "https://api.multica.ai",
    "https://multica.ai",
  );
  const selfHosted = custom({
    id: "srv_self",
    name: "Home lab",
    apiUrl: "https://hp-server.example.test",
    webUrl: null, // 单域名部署:Web 与 API 同源
  });
  const servers = composeServerList(builtIn, [selfHosted]);

  const consume = (activeId: string) => {
    const active = pickActiveServer(servers, activeId);
    const apiUrl = active.apiUrl;
    const webUrl = resolveWebUrl(active);
    return {
      // ① data/api.ts —— fetch 与 uploadFile 的路径拼接
      apiFetch: `${apiUrl}/api/me`,
      // ② realtime-provider —— WSClient 的连接地址
      ws: toWebSocketUrl(apiUrl),
      // ③ lib/attachment-url —— 服务器相对路径的附件
      attachment: `${apiUrl}/api/attachments/att-1/download`,
      // ④ issue/[id] —— Copy link / Open on web
      issueLink: `${webUrl}/acme/issue/RUYI-4`,
      // ⑤ project/[id] —— Open on web
      projectLink: `${webUrl}/acme/projects/p-1`,
      // ⑥ comment-context-menu —— Copy Link
      commentLink: `${webUrl}/acme/issue/RUYI-4#comment-c-1`,
    };
  };

  it("默认(内置项):API 与 Web 分域名,六处各取各的", () => {
    expect(consume(BUILT_IN_SERVER_ID)).toEqual({
      apiFetch: "https://api.multica.ai/api/me",
      ws: "wss://api.multica.ai/ws",
      attachment: "https://api.multica.ai/api/attachments/att-1/download",
      issueLink: "https://multica.ai/acme/issue/RUYI-4",
      projectLink: "https://multica.ai/acme/projects/p-1",
      commentLink: "https://multica.ai/acme/issue/RUYI-4#comment-c-1",
    });
  });

  it("切到单域名自建项:六处全部指向新地址,Web 跳转走回退值", () => {
    expect(consume("srv_self")).toEqual({
      apiFetch: "https://hp-server.example.test/api/me",
      ws: "wss://hp-server.example.test/ws",
      attachment:
        "https://hp-server.example.test/api/attachments/att-1/download",
      issueLink: "https://hp-server.example.test/acme/issue/RUYI-4",
      projectLink: "https://hp-server.example.test/acme/projects/p-1",
      commentLink:
        "https://hp-server.example.test/acme/issue/RUYI-4#comment-c-1",
    });
  });

  it("自建项显式填了 Web 地址:API 三处不动,Web 三处走填写值", () => {
    const withWeb = composeServerList(builtIn, [
      { ...selfHosted, webUrl: "https://app.example.test" },
    ]);
    const active = pickActiveServer(withWeb, "srv_self");
    expect(active.apiUrl).toBe("https://hp-server.example.test");
    expect(resolveWebUrl(active)).toBe("https://app.example.test");
  });
});
