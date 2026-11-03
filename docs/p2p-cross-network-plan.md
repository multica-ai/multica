# P2P 跨网互联方案（局域网 + 腾讯云中继）

基线：chovy-ai/multica `main` @ `5e8f6c72d`（已包含 #58 的局域网 p2p 和设备注册服务）。

## 1. 目标与范围

- daemon 之间支持局域网直连和跨网互联。跨网时先经腾讯云中继连上，再尝试打洞升级为直连；打洞失败就一直走中继。
- 只支持**同一 Multica 账号**下的设备互联。
- 链路：对端 daemon ⇄ 本机 daemon ⇄ 本机 Electron。Electron 不直接接触 p2p。
- p2p-server 独立于 Multica 服务部署，只借用 `api.multica.ai` 的 `/api/me` 做鉴权，不访问 Multica 的数据库。
- 不在本期范围：
  - Electron ⇄ daemon 的转发接口（等第一个业务功能确定后再设计）
  - 跨账号互联
  - 域名和 HTTPS
  - 按账号限速、流量统计

## 2. 已定决策

| 项 | 决定 |
|---|---|
| 注册接口 | 明文 HTTP，`<公网IP>:8090`。已接受 token 明文传输的风险 |
| 鉴权 | Multica token 换取 p2p-server 签发的 24 小时会话凭证，之后的请求都用会话凭证 |
| 中继 | 放在 p2p-server 进程里，负责牵手，也做兜底转发；不限制单条连接的时长和流量 |
| 中继限额 | 每个账号最多同时 8 条中继连接；全局只限制总量 |
| 服务器 | 腾讯云，中国大陆地域，只有公网 IP，没有域名 |
| 部署 | GitHub Actions 构建镜像 → 推送 TCR → 自动化助手在服务器上拉取并重启 → 健康检查，失败自动回滚 |

## 3. 总体架构

```
            腾讯云（中国大陆，公网 IP，无域名）
          ┌───────────────────────────────────────┐
          │ p2p-server（单进程）                    │
          │  ├─ HTTP 注册接口      :8090/tcp（明文） │──── /api/me ───> api.multica.ai
          │  ├─ libp2p 中继节点    :4001/tcp+udp    │
          │  └─ SQLite（设备、会话）                │
          └───────────────────────────────────────┘
               ▲  注册/列表(HTTP)     ▲  中继预约 / 中继转发 / 打洞协调(libp2p)
               │                      │
     ┌─────────┴────────┐    ┌────────┴─────────┐
     │ daemon A         │◄══►│ daemon B         │   直连（局域网 mDNS / 打洞成功）
     │  ▲ 本地接口       │    │  ▲               │   或经中继
     │ Electron A       │    │ Electron B       │
     └──────────────────┘    └──────────────────┘
```

| 通道 | 端口 | 加密 | 内容 |
|---|---|---|---|
| HTTP 注册接口 | 8090/TCP | **无（明文）** | 换取会话凭证、注册设备、列出设备、下发中继地址 |
| libp2p | 4001/TCP + UDP（QUIC） | 有（Noise/TLS） | 中继预约、中继转发、打洞协调（DCUtR）、AutoNAT、identify |

daemon 与 daemon 之间的数据始终端到端加密，中继只转发密文，无法解密。

## 4. 端到端流程

```
daemon A                         p2p-server                         daemon B
   │ POST /v1/sessions (Multica token) │                                  │
   │──────────────────────────────────>│── GET /api/me ──> api.multica.ai │
   │<── session_token (24h) ───────────│                                  │
   │ GET /v1/config                    │                                  │
   │<── 中继地址 ────────────────────────│                                  │
   │ libp2p 连接 4001 + identify        │                                  │
   │<── 观测到的公网地址 ─────────────────│                                  │
   │ 中继预约 Reserve                   │  ACL：peer 已注册？               │
   │<── 预约成功 ────────────────────────│                                  │
   │ POST /v1/devices (公钥 + 地址)     │                                  │
   │──────────────────────────────────>│                                  │
   │                                   │<──── B 做同样的步骤 ───────────────│
   │ GET /v1/devices（同账号设备 + 地址） │                                  │
   │<──────────────────────────────────│                                  │
   │ 1. 先拨 B 的直连地址（局域网、公网、端口映射）                             │
   │ 2. 失败则拨 B 的中继地址 ────────────>│ ACL：同账号？未超 8 条？ ─────────>│
   │<══════════════ 经中继的连接（端到端加密）═══════════════════════════════>│
   │ 3. DCUtR 同时向对方发包打洞 <════════════════════════════════════════════>│
   │    成功：切到直连，中继连接空闲后关闭；失败：一直走中继                       │
```

同一局域网内，mDNS 会先发现对方并直连，不经过中继。

## 5. 鉴权

1. daemon 调 `POST /v1/sessions`，请求头带 `Authorization: Bearer <Multica token>`。
2. p2p-server 调 `api.multica.ai/api/me` 得到 `user_id`，签发**会话凭证**：随机 32 字节，库里只存 SHA-256 哈希，24 小时过期。
3. 其余 `/v1/*` 接口只接受会话凭证，不接受 Multica token。凭证过期或收到 401 时，daemon 重新执行第 1 步。
4. 中继层不使用凭证：设备注册时，p2p-server 从公钥推出 peer ID 并记录它所属的 `user_id`；libp2p 握手本身证明了对方确实持有这个 peer ID。

**明文 HTTP 的风险（已接受）**：能截获流量的人可以拿到 Multica token。用会话凭证后，Multica token 大约每 24 小时才发送一次；截获的会话凭证只能读写这个账号的 p2p 设备列表。以后有了域名，把服务地址改成 `https://` 即可，代码不用改。

## 6. p2p-server 改造

### 6.1 配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `MULTICA_API_URL` | 必填 | 用于 token 校验 |
| `MULTICA_P2P_LISTEN` | `:8090` | HTTP 监听地址 |
| `MULTICA_P2P_DB` | `p2p.db` | SQLite 文件路径 |
| `MULTICA_P2P_KEY` | `p2p-server.key` | **新增**：中继节点私钥。首次启动自动生成，之后保持不变，保证中继 peer ID 固定 |
| `MULTICA_P2P_LIBP2P_PORT` | `4001` | **新增**：中继的 TCP 和 QUIC 端口 |
| `MULTICA_P2P_PUBLIC_IP` | 必填 | **新增**：服务器公网 IP。腾讯云 CVM 的网卡上只有内网 IP，节点自己看不到公网地址，所以必须显式配置后对外宣告 |
| `MULTICA_P2P_RELAY_MAX_CIRCUITS_PER_USER` | `8` | **新增**：每个账号同时经中继的连接数上限 |

### 6.2 HTTP 接口

所有 `/v1/*` 接口的错误响应格式保持现有的 `{"error": "..."}`。

**`POST /v1/sessions`**（新增）

- 请求头：`Authorization: Bearer <Multica token>`，无请求体。
- 200：`{"session_token": "<base64url>", "expires_at": "<RFC3339>"}`
- 401：token 无效。503：`api.multica.ai` 不可用。429：同一 IP 每分钟超过 30 次。

**`GET /v1/config`**（新增，需要会话凭证）

```json
{
  "relay": {
    "peer_id": "12D3KooW...",
    "addrs": [
      "/ip4/<公网IP>/udp/4001/quic-v1",
      "/ip4/<公网IP>/tcp/4001"
    ]
  }
}
```

**`POST /v1/devices`**（需要会话凭证）

- 请求体在现有字段（`daemon_id`、`profile`、`public_key`、`device_name`）上增加 `addrs`：multiaddr 字符串数组，最多 32 条，每条不超过 256 字节。无法解析的地址直接丢弃，不报错。
- 同一个 peer ID 已经被另一个账号注册时返回 409，防止抢占别人的设备身份。

**`GET /v1/devices`**（需要会话凭证）：每个设备增加 `addrs` 字段。

**`DELETE /v1/devices?daemon_id=&profile=`**（需要会话凭证）：不变。删除后立即断开该设备的中继预约和中继连接。

**`GET /healthz`**：检查数据库可读、libp2p 节点在运行，正常返回 204，否则返回 503。

### 6.3 存储

p2p-server 还没有部署过，没有旧库需要兼容，直接修改建表语句即可。

```sql
CREATE TABLE IF NOT EXISTS device (
    user_id     TEXT NOT NULL,
    daemon_id   TEXT NOT NULL,
    profile     TEXT NOT NULL,
    peer_id     TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    device_name TEXT NOT NULL,
    addrs       TEXT NOT NULL DEFAULT '[]',   -- 新增，JSON 数组
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, daemon_id, profile)
);
CREATE INDEX IF NOT EXISTS device_updated_at ON device (updated_at);
CREATE UNIQUE INDEX IF NOT EXISTS device_peer_id ON device (peer_id);  -- 新增，中继 ACL 按 peer ID 查询

CREATE TABLE IF NOT EXISTS session (                                  -- 新增
    token_hash  TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    expires_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS session_expires_at ON session (expires_at);
```

现有的每小时清理任务一并清理过期的会话。

### 6.4 中继节点

- 使用 go-libp2p 的 Circuit Relay v2（`p2p/protocol/circuitv2/relay`），同时开启：
  - **identify**：把 daemon 被观测到的公网地址告诉它，这是打洞的前提。
  - **ping**。
  - 不开 AutoNAT 服务：daemon 侧没有 AutoNAT 客户端（见 7.2）。
- 通过 `AddrsFactory` 对外只宣告 `MULTICA_P2P_PUBLIC_IP` 对应的地址，不宣告内网地址。
- **ACL**（`relay.WithACL`）：
  - `AllowReserve(peer, addr)`：peer ID 已注册且未过期才允许预约。
  - `AllowConnect(src, srcAddr, dst)`：src 和 dst 都已注册、属于同一个 `user_id`，并且该账号当前的中继连接数少于 8 条。
  - 查询结果在内存里缓存 30 秒，避免每次建连都查 SQLite。设备删除时立即清掉对应缓存。
- **按账号计数**：relay v2 的 ACL 拿不到"连接已关闭"的通知，所以不单独维护计数器。每次 `AllowConnect` 时遍历中继节点上当前打开的 hop 协议流（`/libp2p/circuit/relay/0.2.0/hop`），统计其中属于该账号的数量。设备规模小，遍历成本可以忽略。
- **兜底转发**：`Resources.Limit = nil`，不限制单条中继连接的时长和流量。
- **全局总量限制**：

| 参数 | 值 | 说明 |
|---|---|---|
| `MaxReservations` | 1024 | 全局预约总数 |
| `MaxReservationsPerIP` | 16 | 同一出口 IP 的预约数（公司或家庭网络下有多台设备） |
| `MaxCircuits` | 16 | 单个设备同时承载的中继连接数 |
| `ReservationTTL` | 1 小时 | 默认值，daemon 到期前续约 |
| 资源管理器 | 默认的自动伸缩上限 | 兜底限制总连接数、流数和内存 |

## 7. daemon 改造

### 7.1 配置

| 变量 | 说明 |
|---|---|
| `MULTICA_DAEMON_P2P_SERVER_URL` | 已有。设置后开启 p2p；形如 `http://<公网IP>:8090` |
| `MULTICA_DAEMON_P2P_PORT` | 已有。本机直连端口，默认随机 |

中继地址由 `/v1/config` 下发，不新增配置项。

### 7.2 libp2p host（`internal/daemon/p2p/host.go`）

在现有手工拼装的 host 上增加：

- Circuit Relay v2 客户端传输：拨号 `/p2p-circuit` 地址，接收经中继的连接。
- identify：打洞依赖它获取自己的公网地址。
- DCUtR 打洞服务（`p2p/protocol/holepunch`）。
- UPnP/NAT-PMP 端口映射（`basichost` 的 NATManager）：家用路由器支持的话可以直接获得公网地址。

**实施调整**：没有接入 AutoNAT v2 客户端。它需要在 daemon 里再起一个专门回拨用的 libp2p host，而打洞只依赖中继通过 identify 观测到的地址（只要一个观测者即可），不依赖 AutoNAT。状态接口也因此不提供 `reachability` 字段。

host 是手工组装的，必须显式传入 `ObservedAddrsManager`。否则设备在 NAT 后又没有端口映射和公网 IPv6 时，没有公网地址，打洞服务不会启动。观测地址只用于打洞，不会对外登记：libp2p 要求至少 4 个观测者才宣告，而这里只有中继一个。

实测 CLI 体积（darwin/arm64，`-s -w`）：28.76MB → 29.16MB，增加约 393KB。

### 7.3 连接拦截器（`gater.go`）

- 放行 `/v1/config` 返回的中继 peer ID。
- 其余规则不变：经中继进来的连接同样要通过握手后的白名单检查，不是同账号的设备一律拒绝。

### 7.4 中继预约

- 启动并换取会话凭证后，调 `GET /v1/config` 拿到中继地址，然后进入预约循环：
  - 调 `client.Reserve`，在到期前 5 分钟续约。
  - 失败或断线后按 5s、30s、2min、10min 退避重试。
- 预约成功后，本机对外宣告的地址里加上 `<中继地址>/p2p-circuit`。

中继地址通过明文 HTTP 下发，可能被篡改。但中继本来就看不到端到端数据，被换成恶意中继最坏只是连不上，不会泄露数据。

### 7.5 地址上报与建连

- **上报**：订阅 `EvtLocalAddressesUpdated`，地址变化后防抖 250 毫秒再调用 `POST /v1/devices`。上报内容包括局域网地址、端口映射地址，以及中继地址。每 6 小时的定时重新注册保留。
- **刷新白名单**：保持每分钟一次。把注册服务返回的对端 `addrs` 写入 peerstore，对没有连上的同账号设备主动拨号。
- **建连顺序**：libp2p 先试直连地址，失败后走中继；经中继连上后，DCUtR 自动尝试打洞升级。直连出现后成为主连接，没有流的中继连接在下一轮检查（最长 15 秒）时关闭，释放账号的中继额度；中继预约保留，直连失效时仍可回到中继。
- **局域网**：同一局域网内优先直连。设备已走中继时，mDNS 发现对方会立即尝试直连。
- **中继时重试直连**：设备走中继期间，每轮检查对已知直连地址强制直连拨号，失败后从 15 秒退避到 5 分钟。地址变化、mDNS 发现或本机网络变化会清除退避。
- **开数据流**：`Service.Open` 统一加上 `network.WithAllowLimitedConn`，保证经中继的连接也能开流。

#### 网络切换恢复

恢复链路：本机网络变化 → 重建连接并重新注册 → 注册中心通知同账号设备 → 对端刷新地址并唤醒拨号 → 中继先恢复业务 → 桌面重新连接并读取会话。打洞在后台继续，直连建立后沿用 `OnDirect` 迁移已有中继流，不再为每条业务流固定等待 5 秒。

- daemon 每秒读取本机接口地址、路由接口、网关和首选源地址，期间不发送网络探测包。快照变化时立即关闭本地地址已消失的连接，其余连接用 1 秒探测决定去留（VPN 或默认路由变化常常不影响它们；QUIC 共用套接字时本地地址是通配地址，只能靠探测判断）。同时取消正在进行的拨号、中继预约和注册中心请求，关闭注册中心的旧空闲 TCP 连接，清除重试等待并重新注册。libp2p 地址变化事件另以 250 毫秒防抖更新注册。
- 注册中心新增 `GET /v1/events`，用会话凭证鉴权，通过 SSE 发送 `ready` 和 `devices.changed`，`data` 均为 `{}`。注册或删除只通知同账号订阅者；队列合并重复通知。daemon 收到事件后读取权威列表，不使用事件携带地址。流每 20 秒结束以重新校验会话，重连后的 `ready` 补齐断线期间遗漏的变化；失败重试从 0.5 秒递增至 5 秒，本机网络变化可立即打断等待。注册中心仍为单进程部署，未引入跨实例事件总线。
- 地址更新会中断已有拨号及退避，下一次使用最新地址；另触发 1 秒超时的并行存活检查，逐条探测连接，只关闭失效旧路径并立即拨号，保留已建立的健康替代路径。无事件时仍每 15 秒检查，单次超时 5 秒。持续不可达时保留 5、10、20、30 秒退避，账号移除或退出后停止。普通注册请求最长 5 秒，事件流请求最长 25 秒。
- 本机鉴权入口新增 `GET /local/devices/{peer}/local/v2/link-events`，仅向当前桌面发送 `link.connected` / `link.disconnected` SSE，`data` 为 `{}`，不经过远端。链路恢复时立即清除桌面 WebSocket 退避，重新取得一次性票据。释放账号或工作区后取消订阅和待完成握手。旧 daemon 不支持事件接口时继续使用现有重试机制。
- WebSocket 建立后，取消该账号与工作区仍未完成的远端 GET（包括只收到响应头的读取），最多重新读取一次；其他工作区的读取不受影响。释放作用域时取消读取且不重试。POST、消息发送和上传不自动重放。会话列表、当前会话和事件订阅恢复后保留原选择。

验证目标是新网络和注册中心可用后以秒级恢复，不能承诺所有网络下的固定时延；实际网络尚未连通、NAT、丢包或中继故障仍会增加耗时。需要部署新版 p2p-server，并升级两端 daemon 和查看端 Desktop 才能覆盖完整链路。测试使用隔离节点验证中继流在 1 秒截止前可用、设备事件唤醒退避、本机网络变化后 2 秒内重建路径，以及桌面收到链路事件后立即重试；真实 Wi-Fi/热点切换需额外验收。

### 7.6 状态接口

本地健康端口上现有的 p2p 状态接口增加以下字段：

```json
{
  "enabled": true,
  "peer_id": "...",
  "listen_addrs": ["..."],
  "relay": { "peer_id": "...", "reserved": true, "expires_at": "..." },
  "peers": [
    { "peer_id": "...", "device_name": "...", "connected": true, "connection": "direct | relayed", "addrs": ["..."] }
  ]
}
```

### 7.7 会话凭证与退出登录

- 注册服务客户端（`p2p_registry.go`）先换取会话凭证并缓存在内存里，遇到 401 重新换取。
- **退出登录**：`multica auth logout` 在清除 Multica token **之前**，用这个 token 换取会话凭证，然后调 `DELETE /v1/devices` 删除本机设备。p2p-server 连不上时只打印警告，不阻止退出，设备 24 小时后自动过期。
- daemon 正常停止或重启时**不删除**设备，避免每次重启后对端都要重新建连。
- 桌面端的退出登录不走 CLI，而是直接改配置文件，所以新增隐藏命令 `multica daemon p2p-unregister`，由桌面端在清除 token 前调用。
- 已知限制：退出登录时如果连不上 p2p-server，设备记录会保留 24 小时；这期间同一台机器同一个 profile 改用**另一个账号**登录，注册会返回 409，跨网互联要等旧记录过期后才恢复（局域网不受影响）。

### 7.8 桌面端

- `apps/desktop/src/main/daemon-manager.ts` 里的 `desktopSpawnEnv()` 增加 `MULTICA_DAEMON_P2P_SERVER_URL`。
- 桌面端默认使用已部署的 `http://101.33.221.22:8090`，配置位于 `apps/desktop/src/main/p2p-server-env.ts`。构建时可用 `VITE_P2P_SERVER_URL` 覆盖；用户本机设置的 `MULTICA_DAEMON_P2P_SERVER_URL` 优先级最高。独立 CLI 未设置地址时仍关闭 p2p。
- 默认地址使用 HTTP，注册时的登录凭证会经未加密的 HTTP 传输；需要传输加密时应覆盖为 HTTPS 注册服务地址。连接注册服务后，用户仍须在「设置 → 本设备 → 设备互联」中确认允许其他设备访问，才会共享本机会话。
- 退出登录（`daemon:clear-token`）时先执行 `multica daemon p2p-unregister`，再清除 token。重新认证（reauthenticate）不删除设备。

## 8. 部署流水线

**实施调整**：最初实现的是"编译程序 + scp + systemd"，需要 GitHub 持有服务器 SSH 私钥。后来改成 Docker 镜像 + TCR + 腾讯云自动化助手（TAT），GitHub 不再登录服务器，服务器也不需要手动初始化。细节以 [deploy/p2p-server/README.md](../deploy/p2p-server/README.md) 为准。

### 8.1 链路

1. `test`：`go test` p2p-server 相关包。
2. `deploy`（`production` 环境）：用 `deploy/p2p-server/Dockerfile` 构建镜像（约 15MB），推到 TCR 个人版 `ccr.ccs.tencentyun.com/<命名空间>/p2p-server:<sha>`。
3. 用 CAM 子账号密钥调用 TAT `RunCommand`，以 root 在服务器上执行 `deploy/p2p-server/deploy.sh`，轮询执行结果。
4. 从外网检查 `http://<公网IP>:8090/healthz`，失败只告警（GitHub 构建机访问大陆机器不稳定）。

### 8.2 服务器上的 `deploy.sh`

- 幂等：首次运行安装 Docker 和 curl、调大 UDP 缓冲区（QUIC 需要）、创建数据目录和 `/etc/multica-p2p/env`；之后只同步公网 IP。
- 拉取镜像 → 替换容器 → 健康检查最多 30 秒 → 失败时恢复上一个标签；保留最近 5 个镜像。
- 容器使用 host 网络（中继需要 UDP 4001 和客户端真实 IP），只读根文件系统，去掉全部 capabilities，以 uid 10001 运行。
- TCR 仓库设为公开，服务器拉取不需要凭证。

### 8.3 GitHub 配置

| 类型 | 名称 | 内容 |
|---|---|---|
| `production` Secret | `TCR_USERNAME` / `TCR_PASSWORD` | TCR 推送账号（主账号 ID）和镜像仓库密码 |
| `production` Secret | `TENCENTCLOUD_SECRET_ID` / `TENCENTCLOUD_SECRET_KEY` | CAM 子账号密钥，只授权 `tat:RunCommand`、`tat:DescribeInvocationTasks` |
| 仓库变量 | `TCR_NAMESPACE`、`P2P_INSTANCE_ID`、`P2P_PUBLIC_IP` | 命名空间、实例 ID、公网 IP |

### 8.4 腾讯云配置

- 防火墙入站：8090/TCP、4001/TCP、4001/UDP。部署不再需要 22 端口。
- 确认自动化助手 agent 在线。
- 上线前在服务器上测试访问 `api.multica.ai`。

### 8.5 fork 上的 `release.yml`

它继承自上游，打 `v*` tag 时会发布 ghcr 镜像、Helm chart 和 Homebrew，和本方案无关。fork 上不要打 `v*` tag，或者另外决定要不要禁用它。

## 9. 可观测性

- p2p-server 结构化日志：会话签发、设备注册和删除、中继预约、中继连接的建立和拒绝（带拒绝原因：未注册、跨账号、超过上限）。日志里不出现 token，也不出现凭证明文。
- 每 5 分钟打印一次汇总：当前预约数、中继连接数、连接数最多的前几个账号。
- 第二期再考虑 Prometheus 指标和流量统计。

## 10. 测试

- **p2p-server 单元测试**：
  - 会话：签发、过期、过期后 401、Multica token 不能直接调用设备接口。
  - 设备：`addrs` 校验和截断；peer ID 被其他账号占用时返回 409。
  - ACL：未注册设备不能预约；跨账号不能建连；同一账号第 9 条连接被拒绝，关闭一条后可以再建。
- **进程内集成测试**：起一个中继和两个 daemon，都只监听本机回环地址，并禁止它们直接互拨。验证：
  - 只能经中继连上，能开数据流，状态显示 `relayed`。
  - 跨账号的设备连不上。
  - 删除设备后，中继连接被断开。
- **daemon 单元测试**：会话凭证 401 后重新换取；地址变化触发防抖上报；退出登录调用删除接口。
- **手动测试矩阵**：

| 场景 | 预期 |
|---|---|
| 两台设备在同一局域网 | mDNS 直连 |
| 家庭宽带 ⇄ 手机 4G 热点 | 经中继连上，尝试打洞；成功显示 `direct`，失败显示 `relayed` |
| 两端都在对称型 NAT 后 | 一直走中继，数据流正常 |
| 一台设备退出登录 | 对端很快断开，设备列表里不再出现 |
| p2p-server 重启 | daemon 自动重新预约和注册，连接恢复 |
| 一个账号开超过 8 条中继连接 | 第 9 条被拒绝，日志里有原因 |

## 11. 交付拆分

| PR | 内容 | 依赖 |
|---|---|---|
| 1 | p2p-server：会话凭证、地址存储、`/v1/config`、中继节点和 ACL、按账号计数、测试 | — |
| 2 | 部署：`deploy-p2p.yml` 和服务器部署脚本 | PR 1。合并后先把服务部署上去，给 PR 3 联调用 |
| 3 | daemon：中继客户端、打洞、AutoNAT、端口映射、地址上报、状态接口、退出登录清理；桌面端传入服务地址 | PR 1，联调需要 PR 2 |

## 12. 风险

| 风险 | 应对 |
|---|---|
| 明文 HTTP 泄露 token | 已接受；用会话凭证降低暴露频率；有域名后切到 HTTPS |
| 大陆访问 `api.multica.ai` 不稳定 | 会话凭证 24 小时有效，减少调用次数；上线前实测 |
| 兜底转发占用服务器带宽和流量 | 每个账号最多 8 条中继连接，加上全局总量限制；状态接口显示 `relayed`，便于观察 |
| CLI 体积变大 | 只引入中继客户端、打洞和 AutoNAT 需要的包，PR 里写明增量 |
| 对称型 NAT 打洞成功率低 | 预期内，由中继兜底 |
| 中继地址被篡改 | 最坏只是连不上，不泄露数据 |

## 13. 待定事项（方案落地后再定）

1. 服务器公网 IP 和带宽规格。
2. 在服务器上执行初始化脚本，并在 GitHub 上配置第 8.3 节的密钥。
3. 在服务器上测试访问 `api.multica.ai` 的结果。
4. 是否禁用 fork 上的 `release.yml`。
5. 第一个使用 p2p 链路的业务功能，以及 Electron ⇄ daemon 的转发接口设计。
