# WuKongIM 协议栈 — Go 移植对照表

> 目标：把 `cc-channel-octo/src/octo/{socket,types,api}.ts`（1512 行 TS）移植为 multica
> 内置的 `server/internal/integrations/im` 包传输层（Go）。本文是逐函数蓝图。
>
> 参考实现（按可读性排序）：
> - `cc-channel-octo/src/octo/socket.ts`（875 行，已导出 Encoder/Decoder，注释最全）
> - `cc-channel-octo/src/octo/types.ts`（145 行，消息/帧结构）
> - `cc-channel-octo/src/octo/api.ts`（492 行，REST：register/sendMessage/...）
> - 交叉参考 Python 版：`hermes-channel-octo/src/hermes_octo_plugin/protocol.py`
>
> Octo User Bot 接入路径（已 spike 确认）：
> `bf_*` token → `POST /v1/bot/register` 拿 `im_token`+`ws_url` → WuKongIM WS 长连接收消息；
> 出站全走 REST（sendMessage / message-edit / typing）。

## 0. 包布局与依赖

```
server/internal/integrations/im/
  codec.go      Encoder/Decoder + 变长整数 + 粘包拆帧        (~250 行)
  crypto.go     X25519 ECDH + AES-128-CBC 解密 + MD5 派生     (~90 行)
  packet.go     PacketType 常量 + 各帧 encode/decode          (~200 行)
  socket.go     连接生命周期 + 心跳 + 重连 + RECVACK + 分发    (~350 行)
  types.go      BotMessage/MessagePayload/Mention/ChannelType  (~120 行，对照 types.ts)
  http_client.go REST：register/sendMessage/editMessage/typing/heartbeat/groupMembers (~250 行)
```

**Go 标准库即可覆盖全部 crypto，无需第三方：**

| TS 依赖 | Go 替代 | 说明 |
|---|---|---|
| `curve25519-js` (`generateKeyPair`/`sharedKey`) | `crypto/ecdh` 的 `ecdh.X25519()` | Go 1.26 标准库自带；`NewPrivateKey` 内部按 RFC 7748 做 clamping，与 curve25519-js 行为一致 |
| `crypto-js` (AES-CBC) | `crypto/aes` + `crypto/cipher`（`NewCBCDecrypter`） | PKCS7 去填充需手写（~10 行） |
| `md5-typescript` | `crypto/md5` | `Md5.init(s)` 返回 32 位小写 hex 字符串 → `hex.EncodeToString(md5.Sum(...))` |
| `ws` | `github.com/gorilla/websocket` | multica 已用（realtime hub 即 gorilla） |
| `buffer`/`TextEncoder` | `[]byte` / `string()` | 原生 |

> ⚠️ **MD5 派生务必逐字节对齐**：TS `Md5.init(secretBase64)` 的输入是 *共享密钥的 base64 字符串*，
> 不是原始字节；输出取前 16 个 hex 字符当 AES key。见 §3 crypto.go。

---

## 1. codec.go — 二进制编解码 + 拆帧

### 1.1 Encoder（对照 socket.ts:58-80）

大端写入。Go 用 `encoding/binary.BigEndian` 即可，但为逐行对照保留方法名。

| TS 方法 | Go 实现要点 |
|---|---|
| `writeByte(b)` | `buf = append(buf, byte(b))` |
| `writeInt16(b)` | `binary.BigEndian.PutUint16` |
| `writeInt32(b)` | `binary.BigEndian.PutUint32` |
| `writeInt64(n bigint)` | `binary.BigEndian.PutUint64`；注意 messageID 是 *无符号 int64*，用 `uint64` |
| `writeString(s)` | 写 `int16` 长度（UTF-8 字节数）+ 字节；空串写长度 0。**长度是字节数不是字符数** |
| `toUint8Array()` | 返回 `[]byte` |

```go
type Encoder struct{ buf []byte }
func (e *Encoder) WriteByte(b byte)          { e.buf = append(e.buf, b) }
func (e *Encoder) WriteUint16(v uint16)      { e.buf = binary.BigEndian.AppendUint16(e.buf, v) }
func (e *Encoder) WriteUint32(v uint32)      { e.buf = binary.BigEndian.AppendUint32(e.buf, v) }
func (e *Encoder) WriteUint64(v uint64)      { e.buf = binary.BigEndian.AppendUint64(e.buf, v) }
func (e *Encoder) WriteString(s string) {
    b := []byte(s)
    e.WriteUint16(uint16(len(b)))
    e.buf = append(e.buf, b...)
}
```

### 1.2 Decoder（对照 socket.ts:82-171）

**核心安全点（必须移植，否则恶意/损坏帧会 panic 或静默错读）：**
- 每个 read 前 `require(n)` 边界检查 → Go 返回 `error`，不要 panic（用 error 让上层 try/catch 等价物 `if err != nil` 拒帧）。
- `readString`：先读 int16 长度，再 `require(len)` 防止超长 length 越界读。

| TS 方法 | Go 签名 | 备注 |
|---|---|---|
| `require(n)` | `func (d *Decoder) require(n int) error` | offset+n > len → 返回 error |
| `readByte()` | `(byte, error)` | |
| `readInt16()` | `(uint16, error)` | |
| `readInt32()` | `(uint32, error)` | TS 末尾 `>>> 0` 转无符号 → Go 直接 uint32 |
| `readInt64String()` | `(string, error)` | 8 字节大端 → `strconv.FormatUint(uint64, 10)`；messageID 用字符串传递避免精度问题（与 TS 一致） |
| `readInt64BigInt()` | `(uint64, error)` | timeDiff/nodeId 等丢弃字段 |
| `readString()` | `(string, error)` | |
| `readRemaining()` | `[]byte` | 剩余全部（加密 payload） |
| `readVariableLength()` | `(int, error)` | MQTT 变长，见下 |

> Go 风格建议：Decoder 可改为持有 `err` 字段的 "sticky error" 模式（一旦出错后续 read 都 no-op），
> 减少每行 `if err != nil`。但首版建议老实返回 error，对照更直观，稳定后再重构。

### 1.3 变长整数（MQTT remaining-length，对照 socket.ts:160-191）

```go
// encodeVariableLength: len → []byte（7 位一组，高位 0x80 表示后续还有）
func encodeVarLen(n int) []byte {
    if n == 0 { return []byte{0} }
    var ret []byte
    for n > 0 {
        d := n % 0x80
        n /= 0x80
        if n > 0 { d |= 0x80 }
        ret = append(ret, byte(d))
    }
    return ret
}
```

解码侧在拆帧函数里内联（见 §1.4），**必须带 `MAX_VARLEN_BYTES=4` 上限**（socket.ts:43, 637）：
超过 4 个延续字节即判定畸形帧 → 返回 error。否则一串 `0x80` 能让 buffer 无限增长。

### 1.4 拆帧 unpackOne（对照 socket.ts:554-659）★最易错，重点移植

TS 用 `tempBuffer: number[]` 累积 + moving cursor 避免 O(n²)。Go 用 `[]byte` + 已消费偏移。

**算法（单帧解析，返回消费字节数；0 表示帧不完整需等更多数据）：**
1. `available = len(data) - start`；≤0 → return 0
2. `header = data[start]`；`packetType = header >> 4`
3. PONG(8) → 处理后 return 1（单字节）；PING(7) → return 1
4. 从 `start+1` 读变长 remaining-length：
   - 越界（pos 超出 buffer）→ return 0（等更多字节）
   - 延续字节数 ≥ MAX_VARLEN_BYTES → return error（畸形）
5. `totalLength = 1 + remLenBytes + remLength`；> available → return 0（不完整）
6. 切出 `data[start:start+totalLength]` → `onPacket(packet)` → return totalLength

**外层 handleRawData（socket.ts:554-595）：**
- 追加新字节到 buffer
- buffer 超 `MAX_TEMP_BUFFER_BYTES=1MiB`（socket.ts:36）→ 清空 + 关连接重连（防 OOM）
- 循环 `unpackOne` 推进 cursor，最后一次性 `buffer = buffer[consumed:]`
- 任一帧 error → 清空 buffer + 关连接（让重连重新握手）

```go
const (
    maxTempBufferBytes = 1 << 20 // 1 MiB
    maxVarLenBytes     = 4
)
```

---

## 2. packet.go — 帧 encode/decode

### 2.1 常量（对照 socket.ts:16-28）

```go
type PacketType byte
const (
    pktConnect    PacketType = 1
    pktConnack    PacketType = 2
    pktSend       PacketType = 3 // bot 不发，忽略
    pktSendack    PacketType = 4 // 忽略
    pktRecv       PacketType = 5 // ★入站消息
    pktRecvack    PacketType = 6 // ★我方确认
    pktPing       PacketType = 7
    pktPong       PacketType = 8
    pktDisconnect PacketType = 9
)
const protoVersion = 4
```

### 2.2 encodeConnectPacket（对照 socket.ts:213-238）

body 字段**顺序严格**：version(byte) → deviceFlag(byte) → deviceID(str) → uid(str) →
token(str) → clientTimestamp(int64) → clientKey(str, base64 公钥)。
帧 = `header byte((Connect<<4)|0)` + `varlen(len(body))` + body。

- `deviceFlag = 0`（app/bot）
- `deviceID = generateDeviceID() + "W"`（§5）
- `clientTimestamp = 毫秒时间戳`（Go 里需注入，见 §6 关于 multica 禁 `time.Now()` 的注意）
- `clientKey = base64(公钥32字节)`

### 2.3 encodePingPacket（socket.ts:240-242）

单字节：`(pktPing<<4)|0`。

### 2.4 encodeRecvackPacket（socket.ts:244-255）★每条消息必发

body = `writeInt64(messageID)` + `writeInt32(messageSeq)`。
帧 = header `(pktRecvack<<4)|0` + varlen + body。
**messageID 是字符串形式的 uint64**，转换：`strconv.ParseUint(messageID, 10, 64)`。

### 2.5 settingByte 解析（socket.ts:257-269）

```go
// 只建模 adapter 真正用到的位（topic 决定是否多读一个 wire 字段；streamOn 标记流式）。
// receipt-enabled 等其他位忽略。
type settingFlags struct{ topic, streamOn bool }
func parseSetting(v byte) settingFlags {
    return settingFlags{
        topic:    (v>>3)&1 > 0,
        streamOn: (v>>1)&1 > 0, // ★ streamOn → BotMessage.StreamOn
    }
}
```

---

## 3. crypto.go — ECDH + AES + MD5（对照 socket.ts:195-209, 705-748）★最易出静默 bug

### 3.1 DH 密钥对（连接时，socket.ts:398-401）

```go
import "crypto/ecdh"
curve := ecdh.X25519()
priv, _ := curve.GenerateKey(rand.Reader) // 等价 generateKeyPair(randomBytes(32))
pubB64 := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())
// pubB64 放进 CONNECT 的 clientKey
```

### 3.2 CONNACK 后派生 AES key/IV（socket.ts:717-748）★逐字节对齐

CONNACK body 读取顺序（onConnack, socket.ts:705-715）：
1. if `hasServerVersion`（header bit0）→ `serverVersion = readByte()`
2. `readInt64BigInt()` // timeDiff 丢弃
3. `reasonCode = readByte()`
4. `serverKey = readString()` // 服务端公钥 base64
5. `salt = readString()`
6. if `serverVersion >= 4` → `readInt64BigInt()` // nodeId 丢弃

`reasonCode == 1` 才成功，然后：

```go
// ① salt 字节长度 < 16 → 握手失败重连（socket.ts:728-740）
//    否则 AES IV 错误，会"连上但每条消息静默解密失败"
if len([]byte(salt)) < 16 { /* close + reconnect */ }

// ② 共享密钥
serverPub, _ := curve.NewPublicKey(mustBase64Decode(serverKey))
secret, _ := priv.ECDH(serverPub)                       // sharedKey(dhPriv, serverPub)
secretB64 := base64.StdEncoding.EncodeToString(secret)

// ③ AES key = MD5(secretB64) 的前 16 个 hex 字符（socket.ts:744-745）
sum := md5.Sum([]byte(secretB64))
fullHex := hex.EncodeToString(sum[:])  // 32 字符小写 hex
aesKey := fullHex[:16]                  // 16 字符 → 16 字节 ASCII

// ④ AES IV = salt 的前 16 字节（socket.ts:748）
aesIV := string([]byte(salt)[:16])
```

> `reasonCode == 0` → 被踢（needReconnect=false）；其他 → 连接失败。

### 3.3 AES-CBC 解密（socket.ts:195-209）

TS 流程：payload bytes → binary string → base64 parse 成密文 → AES-CBC 解密 → UTF-8 → JSON。
**注意 TS 的绕弯**：`aesDecrypt` 收到的 `encryptedPayload` 本身就是 base64 编码的密文字节
（`onRecv` 里 `readRemaining()` 拿到的是 base64 文本的字节）。Go 实现：

```go
func aesDecrypt(payload []byte, key, iv string) ([]byte, error) {
    ct, err := base64.StdEncoding.DecodeString(string(payload)) // payload 是 base64 文本
    if err != nil || len(ct)%aes.BlockSize != 0 { return nil, errBadCipher }
    block, _ := aes.NewCipher([]byte(key))         // key 16 字节
    mode := cipher.NewCBCDecrypter(block, []byte(iv)) // iv 16 字节
    out := make([]byte, len(ct))
    mode.CryptBlocks(out, ct)
    return pkcs7Unpad(out)                          // 手写去填充
}
```

> ⚠️ **务必先用一条真实抓包数据做单元测试对齐**（见 §7）：key/IV/base64 任一环节字节不对，
> 就是"心跳正常但所有消息解不开"的静默故障——这是协议移植最常见的坑，socket.ts 注释反复强调。

---

## 4. socket.go — 连接生命周期（对照 socket.ts:271-865）

### 4.1 结构体字段（socket.ts:293-323）

```go
type Socket struct {
    opts        SocketOptions // wsUrl, uid, token, onMessage, onConnected/onDisconnected/onError
    conn        *websocket.Conn
    connected   bool
    needReconn  bool

    // 重连/稳定性
    reconnectAttempts   int
    lastConnectUnixMs   int64
    rapidDisconnectN    int

    // crypto（CONNACK 后填）
    aesKey, aesIV string
    dhPriv        *ecdh.PrivateKey
    serverVersion int

    // 拆帧缓冲
    tempBuffer []byte

    // 毒消息计数（按 messageID）
    decryptFail map[string]int

    mu sync.Mutex // Go 并发：读循环 goroutine vs 定时器 goroutine
}
```

> **Go 并发模型差异**：TS 是单线程事件循环；Go 需要一个 read goroutine（`conn.ReadMessage()` 阻塞循环）
> + 一个 heartbeat ticker goroutine + 重连。共享状态（connected/conn/tempBuffer/decryptFail）用 `mu` 保护，
> 或用单 goroutine + channel 串行化（更推荐，避免锁）。建议：read loop 把原始帧丢进 channel，
> 由单个 "处理 goroutine" 消费 → 天然串行，贴近 TS 语义。

### 4.2 方法对照表

| TS 方法 | socket.ts 行 | Go 要点 |
|---|---|---|
| `connect()` | 326-329 | `needReconn=true` → `doConnect()` |
| `disconnect()` | 332-344 | 优雅关闭，停所有 timer，`needReconn=false` |
| `disconnectAndWait()` | 346-378 | 关旧连接并等 close；Go 用 `conn.Close()` + read goroutine 退出确认 |
| `doConnect()` | 382-469 | gorilla `websocket.DefaultDialer.Dial(wsUrl)`；连上后发 CONNECT 帧；起 read goroutine |
| `scheduleReconnect()` | 471-485 | 指数退避 `min(3s·2^n, 60s)` + ±25% 抖动。**Go 禁 `math/rand` 默认源用法见 §6** |
| `startStableTimer()` | 494-502 | 连稳 30s 后清零 reconnectAttempts/rapidDisconnect |
| `restartHeart()` | 513-537 | `time.NewTicker(60s)`；每 tick `pingRetry++`，超 3 次判定 ping 超时→关连接重连；否则发 PING |
| `stopHeart()` | 539-544 | `ticker.Stop()` |
| `sendRaw()` | 548-552 | `conn.WriteMessage(websocket.BinaryMessage, data)`（gorilla 写需加锁，单写 goroutine 或 mu） |
| `handleRawData()` | 554-595 | §1.4 拆帧；buffer 超限或 decode error → 清空+关连接 |
| `unpackOne()` | 604-659 | §1.4 |
| `onPong()` | 663-665 | `pingRetry=0` |
| `onPacket()` | 667-703 | 读 header + varlen 跳过，按 packetType 分发到 onConnack/onRecv/onDisconnect |
| `onConnack()` | 705-768 | §3.2 派生 key/IV；成功 → connected=true + restartHeart + startStableTimer + onConnected |
| `onRecv()` | 770-851 | §4.3 ★ |
| `onDisconnect()` | 853-864 | 服务端踢；needReconn=false + onError |

### 4.3 onRecv — 入站消息解析（socket.ts:770-851）★核心业务入口

读取顺序（严格）：
```
settingByte(byte) → parseSetting
msgKey(str, 丢弃)
fromUID(str)
channelID(str)
channelType(byte)
if serverVersion>=3: expire(int32, 丢弃)
clientMsgNo(str, 丢弃)
messageID(int64→string)
messageSeq(int32)
timestamp(int32)
if setting.topic: topic(str, 丢弃)
encryptedPayload = readRemaining()
```

**关键时序：先解密+解析成功，再发 RECVACK**（socket.ts:790-830）：
- 解密/JSON 失败 → 按 messageID 计数：
  - < `MAX_DECRYPT_RETRIES=3`：**不 ack**，留给服务端重投（处理瞬时抖动）
  - ≥ 3：ack-and-drop（毒消息，避免无限重投堵塞流），`decryptFail` 删除该 id
  - `decryptFail` map 超 `MAX_DECRYPT_FAIL_ENTRIES=1000` 整个清空（内存上限）
- 成功 → 清该 id 计数 → 发 RECVACK → 组装 `BotMessage` → `opts.onMessage(msg)`

`BotMessage` 组装：payload 先 `{type, content, ...原始 JSON 所有字段}`，`StreamOn = setting.streamOn`。

---

## 5. types.go（对照 types.ts 全文）

直接翻译，无逻辑。重点：

```go
type ChannelType uint8
const ( ChannelDM ChannelType = 1; ChannelGroup ChannelType = 2; ChannelTopic ChannelType = 5 )

type MessageType int
const ( MsgText MessageType = 1; MsgImage = 2; /* ... */ MsgRichText = 14 )

type BotMessage struct {
    MessageID   string
    MessageSeq  uint32
    FromUID     string
    ChannelID   string
    ChannelType ChannelType
    Timestamp   uint32
    Payload     MessagePayload
    StreamOn    bool
}

type MentionPayload struct {
    UIDs     []string       `json:"uids,omitempty"`
    Entities []MentionEntity `json:"entities,omitempty"`
    All      any            `json:"all,omitempty"`    // bool|number，服务端遗留双写
    Humans   any            `json:"humans,omitempty"` // 三态 mention
    AIs      any            `json:"ais,omitempty"`
}

// MessagePayload 需保留未知字段 → 用 map 兜底或自定义 UnmarshalJSON
type MessagePayload struct {
    Type    MessageType    `json:"type"`
    Content string         `json:"content,omitempty"`
    Mention *MentionPayload `json:"mention,omitempty"`
    Reply   *ReplyPayload  `json:"reply,omitempty"`
    Extra   map[string]any `json:"-"` // 对应 TS 的 [key:string]:unknown
}
```

> mention 三态（humans/ais/all）语义由 **Octo 服务端**裁定，adapter 只读不判。
> 群聊 @bot 判断：`payload.mention.uids` 是否含 bot 自己的 robot_id（在 dispatcher 层做，不在 socket 层）。

---

## 6. http_client.go（对照 api.ts）

纯 REST，无协议难点。`Authorization: Bearer <bot_token>`（用原始 `bf_*` token，**不是 im_token**）。

| 函数 | 端点 | 关键字段 |
|---|---|---|
| `Register(forceRefresh)` | `POST /v1/bot/register[?force_refresh=true]` | 返回 `{robot_id, im_token, ws_url, api_url, owner_uid, owner_channel_id}` |
| `SendMessage` | `POST /v1/bot/sendMessage` | body: `{channel_id, channel_type, payload:{type:1,content,mention?,reply?}, client_msg_no}` |
| `EditMessage` | `POST /v1/bot/message/edit` | body: `{message_id, message_seq?, channel_id, channel_type, content_edit}`（仅 RichText 整体替换，用于流式）|
| `SendTyping` | `POST /v1/bot/typing` | `{channel_id, channel_type}` |
| `SendHeartbeat` | `POST /v1/bot/heartbeat` | `{}` |
| `GetGroupMembers` | `GET /v1/bot/groups/{groupNo}/members` | 判断 from_uid 是否人类（robot 字段）|
| `GetChannelMessages` | `POST /v1/bot/messages/sync` | 冷启动回填历史；payload 是 base64(JSON)，需解码 |

**移植注意：**
- `client_msg_no` = `uuid.NewString()`（幂等键，服务端去重）。
- **int64 精度保护**：TS `parseOctoJson` 把 16+ 位的 `message_id` 数字转字符串再 parse。
  服务端 `MsgSendResp.MessageID` 是 **裸 int64 数字**（octo-lib `config/msg.go:810`），
  ⚠️ 不能直接 decode 进 string 字段——`json.Unmarshal` 会报
  `cannot unmarshal number into ... string`，`UseNumber()` 也救不了（它只影响 decode 进
  `interface{}`）。正确做法：`SendMessageResult` 自定义 `UnmarshalJSON`，用 `json.RawMessage`
  取 `message_id` 原始字节再 `bytes.Trim(.., '"')`，同时兼容数字和字符串两种 wire 格式。
- 错误响应可能回显 Authorization → 日志脱敏（api.ts:174-177 的 `Bearer ***` 处理）。

---

## 7. 移植验证策略（先写测试，避免静默故障）

1. **codec 单测**：对照 socket.ts 的 Encoder/Decoder（已导出，便于抓黄金样本）。
   写 round-trip：encode CONNECT/RECVACK/PING → 字节级 diff 与 TS 输出一致。
2. **crypto 黄金样本**：从一次真实握手抓 `serverKey`/`salt`/`dhPriv`，固定输入断言
   `aesKey`/`aesIV` 与 TS 计算结果逐字节相等。**这是最高优先级的测试**——key/IV 错会静默吞消息。
3. **拆帧测试**：构造粘包（两帧拼一个 buffer）、半包（帧切两半分两次喂）、超长 varlen、超 1MiB
   → 验证 unpackOne 行为与 socket.ts 一致。
4. **解密毒消息**：喂一个解不开的 payload，验证 <3 次不 ack、=3 次 ack-and-drop。
5. **端到端 PoC**：连真实 Octo（User Bot bf_ token）→ register → WS 连上 → 私聊 bot 一条 →
   日志打出 BotMessage → REST 回一条。这是 spike 的最终验收。

---

## 8. multica 代码规范注意（移植时遵守）

- **禁 `time.Now()` / `math/rand` 全局源？** —— 这是 *workflow 脚本* 的限制，不适用于普通 Go 代码。
  普通后端代码正常用 `time.Now()`、`math/rand`。重连抖动用 `rand.Float64()` 即可。
- **comment 用英文**（CLAUDE.md 硬规则）。本文是设计稿用中文；落地代码注释写英文。
- **gorilla/websocket 已在依赖里**（realtime hub 用的就是它），无需新增。
- **crypto/ecdh、crypto/aes、crypto/md5、encoding/binary、encoding/base64** 全是标准库，零新增第三方依赖。
- 包名 `im`（按用户决定，替代上游的 `octo`/`wukong`）。对外类型加 `IM` 前缀或靠包名限定（`im.Socket`、`im.BotMessage`）。

---

## 附：移植工作量估算

| 文件 | 预估行数（Go） | 难度 | 风险点 |
|---|---|---|---|
| codec.go | ~250 | 中 | 拆帧 O(n) + varlen 上限 |
| crypto.go | ~90 | **高** | MD5/key/IV 逐字节对齐（静默故障源）|
| packet.go | ~200 | 中 | 字段顺序 |
| socket.go | ~350 | **高** | Go 并发模型重构（read goroutine + ticker + 重连）|
| types.go | ~120 | 低 | MessagePayload 未知字段保留 |
| http_client.go | ~250 | 低 | 纯 REST |
| **合计** | **~1260** | | 与 spike 估算 ~1000-1300 行吻合 |

> socket.go 与 crypto.go 是两块硬骨头：前者因 Go 并发模型与 TS 单线程不同需重构，
> 后者因加密逐字节对齐易出静默 bug。建议这两个文件配套测试先行（§7.1-7.4）。
