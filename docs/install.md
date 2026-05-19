# Multica Runtime 安装指引（给 Agent 读的）

你正在帮用户把这台机器注册成 Multica 的 Runtime（执行节点）。
Multica 入口：<https://multica.lilithgames.com>

请按下面的流程执行：

## 步骤 1：选择安装模式

先**问用户一句**："你想装 CLI 模式还是 Desktop 模式？"

- **CLI 模式**：纯命令行 daemon，跑在终端 / 服务器上。适合机房机器、开发机后台跑 agent。
- **Desktop 模式**：图形界面客户端，自带 daemon。适合个人 Mac 平时用。

不确定就推荐 **CLI 模式**。下面两条路径选一条执行。

---

## 路径 A：CLI 模式

### A.1 安装 multica CLI

**macOS**（首选 brew）：

```bash
brew install multica
```

**Linux 或非 brew 环境**（自动识别架构）：

```bash
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # darwin or linux
ARCH=$(uname -m)                              # x86_64 or arm64
if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; fi

curl -fsSL "https://multica.lilithgames.com/api/downloads/cli/${OS}/${ARCH}" \
  -o /tmp/multica.tar.gz
tar -xzf /tmp/multica.tar.gz -C /tmp multica
sudo mv /tmp/multica /usr/local/bin/multica
rm /tmp/multica.tar.gz
```

装完用 `multica version` 验证。

### A.2 已安装用户：升级 multica CLI

如果机器上已经装过 `multica`，先升级 CLI。小队、活动上报等新功能依赖新版 CLI；只重启 daemon 不会更新本机二进制。

**macOS（brew）**：

```bash
brew update
brew upgrade multica || brew install multica
hash -r
```

**Linux 或非 brew 环境**：

按 A.1 的 Linux 安装命令重新下载最新包，并覆盖 `/usr/local/bin/multica`。

升级后验证版本和小队命令：

```bash
which multica
multica version
multica squad activity --help
```

如果 daemon 已经在运行，升级后必须重启 daemon，让它使用新的 CLI：

```bash
multica daemon restart
multica daemon status
```

### A.3 初始化 + 登录 + 启动 daemon

```bash
multica setup self-host \
  --server-url https://multica.lilithgames.com/ \
  --app-url https://multica.lilithgames.com/

multica login              # 浏览器跳转完成谷歌登录
multica daemon start
multica daemon status      # 确认 daemon 在跑
multica runtime list       # 确认 runtime 已注册并 online
```

### A.4 装一个 provider CLI

Multica 的 Agent 真正干活靠 provider CLI。装其中一个即可（用户已有任意一个就跳过）：

- `claude` — Claude Code
- `opencode` — OpenCode
- `openclaw` — OpenClaw

装完确认它在 `PATH` 里能跑起来。

完成。回到 Multica 网页 Settings → Runtimes 应该能看到这台机器。

### A.5 配置开机自启动（必须）

CLI 模式必须配置开机自启动。`multica daemon start` 启动的后台进程不会跟随系统重启自动恢复；如果跳过这一步，机器重启后 Runtime 会离线，Agent 任务不会继续执行。

按系统选一段执行。**前提**：A.1～A.4 已经跑通，`multica daemon status` 当前是 running。

#### macOS（launchd LaunchAgent）

1. 拿到 `multica` 的绝对路径（launchd 的 `PATH` 很短，必须写全路径）：

   ```bash
   which multica
   ```

2. 创建 `~/Library/LaunchAgents/com.multica.daemon.plist`，把 `ProgramArguments` 第一项换成上一步的输出，`PATH` 里追加 provider CLI（`claude`/`opencode` 等）所在目录（用 `which claude` 等命令查）：

   ```xml
   <?xml version="1.0" encoding="UTF-8"?>
   <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
   <plist version="1.0">
   <dict>
     <key>Label</key><string>com.multica.daemon</string>
     <key>ProgramArguments</key>
     <array>
       <string>/opt/homebrew/bin/multica</string>
       <string>daemon</string>
       <string>start</string>
       <string>--foreground</string>
     </array>
     <key>EnvironmentVariables</key>
     <dict>
       <key>PATH</key>
       <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
     </dict>
     <key>RunAtLoad</key><true/>
     <key>KeepAlive</key><true/>
     <key>StandardOutPath</key><string>/tmp/multica-daemon.out.log</string>
     <key>StandardErrorPath</key><string>/tmp/multica-daemon.err.log</string>
   </dict>
   </plist>
   ```

   关键点：
   - 必须带 `--foreground`。`multica daemon start` 默认会自己 fork 一次再退出父进程，launchd 看到父进程立刻退出就会无限重启
   - `PATH` 一定要覆盖 provider CLI 所在目录。装在 nvm / pnpm / `~/.local/bin` 里的 `claude` 等，要把对应目录追加上去
   - `KeepAlive=true` 让 daemon 崩溃后自动拉起；`RunAtLoad=true` 让登录后立即启动

3. 先停掉手动启动的 daemon，避免和 launchd 启动的实例冲突，然后加载：

   ```bash
   multica daemon stop

   launchctl unload ~/Library/LaunchAgents/com.multica.daemon.plist 2>/dev/null
   launchctl load -w ~/Library/LaunchAgents/com.multica.daemon.plist
   ```

   `-w` 让配置持久化，下次开机自动加载。

4. 验证：

   ```bash
   launchctl list | grep com.multica.daemon    # 第一列是 PID，不为 -
   multica daemon status
   multica runtime list                         # 仍然 online
   ```

   上面三条都通过后，CLI 模式安装才算完成。

   想看实时日志：`tail -f /tmp/multica-daemon.err.log` 或 `multica daemon logs -f`。

   卸载（停止开机自启）：

   ```bash
   launchctl unload -w ~/Library/LaunchAgents/com.multica.daemon.plist
   rm ~/Library/LaunchAgents/com.multica.daemon.plist
   ```

#### Linux（systemd user service）

1. 拿到 `multica` 的绝对路径：

   ```bash
   which multica
   ```

2. 创建 `~/.config/systemd/user/multica-daemon.service`，把 `ExecStart` 的路径换成上一步的输出，`Environment=PATH=...` 追加 provider CLI 所在目录：

   ```ini
   [Unit]
   Description=Multica local agent runtime daemon
   After=network-online.target
   Wants=network-online.target

   [Service]
   Type=simple
   ExecStart=/usr/local/bin/multica daemon start --foreground
   Restart=on-failure
   RestartSec=5
   Environment=PATH=/usr/local/bin:/usr/bin:/bin:%h/.local/bin

   [Install]
   WantedBy=default.target
   ```

   同样要带 `--foreground`，原因同 macOS 段。

3. 先停掉手动启动的 daemon，然后启用并启动服务：

   ```bash
   multica daemon stop

   systemctl --user daemon-reload
   systemctl --user enable --now multica-daemon.service
   ```

4. 服务器场景（机器没人图形登录），打开 linger 让 user service 在用户未登录时也跑：

   ```bash
   sudo loginctl enable-linger "$USER"
   ```

5. 验证：

   ```bash
   systemctl --user status multica-daemon.service   # active (running)
   multica daemon status
   multica runtime list
   ```

   上面三条都通过后，CLI 模式安装才算完成。

   查日志：

   ```bash
   journalctl --user -u multica-daemon.service -f
   ```

   卸载：

   ```bash
   systemctl --user disable --now multica-daemon.service
   rm ~/.config/systemd/user/multica-daemon.service
   ```

> Desktop 模式（路径 B）自带"登录后启动"开关，在系统的"登录项"里勾选 Multica 即可，不需要写 plist / unit。

---

## 路径 B：Desktop 模式

### B.1 下载 Desktop 客户端

<https://multica.lilithgames.com/download>

按系统选 Mac / Windows 安装包，装好。

### B.2 改本地配置

编辑 `~/.multica/desktop.json`，写入：

```json
{
  "schemaVersion": 1,
  "apiUrl": "https://multica.lilithgames.com",
  "wsUrl": "wss://multica.lilithgames.com/ws",
  "appUrl": "https://multica.lilithgames.com"
}
```

文件不存在就直接创建。

### B.3 重启 Desktop，登录

完全退出 Desktop 后重开，点谷歌登录按钮完成认证。

完成。Multica 网页 Settings → Runtimes 应该能看到这台机器。

---

## 域名变更后如何更新

如果 Multica 的服务端域名换了（例如从旧域名迁到 `https://multica.lilithgames.com`），**已经装好的 runtime 不会自动跟着切**——必须把客户端配置里指向旧域名的 server / app / ws URL 全部换成新的，否则 daemon 仍然连旧地址，runtime 会显示离线。

### CLI 模式

重新跑一次 `setup self-host`，把新域名写进配置；命令会提示覆盖已有配置，回 `y` 即可。然后重新登录、重启 daemon：

```bash
multica setup self-host \
  --server-url https://multica.lilithgames.com/ \
  --app-url https://multica.lilithgames.com/

multica login
multica daemon restart
multica runtime list       # 确认 runtime 重新 online
```

如果使用了 profile（`--profile`），需要带上同一个 profile 名再跑一次 `setup self-host`。

### 升级后出现 401 / invalid token

如果 `~/.multica/logs/daemon.log` 里出现下面这类日志，说明本地保存的登录 token 已经失效：

```text
GET /api/workspaces returned 401: {"error":"invalid token"}
```

重新登录即可：

```bash
multica daemon stop
multica auth logout
multica login
multica auth status
multica daemon start
multica runtime list
```

如果当前机器没有浏览器，用平台页面提供的 token 登录方式重新登录：

```bash
multica daemon stop
multica auth logout
multica login --token
multica daemon start
multica runtime list
```

### Desktop 模式

编辑 `~/.multica/desktop.json`，把里面的 `apiUrl` / `wsUrl` / `appUrl` 改成新域名（参考 B.2 的格式），保存后**完全退出 Desktop**（macOS 用 `Cmd+Q`，不要只关窗口），再打开重新登录。

---

## 验收

不管走哪条路径，最后请用户在浏览器打开 <https://multica.lilithgames.com>，进 Settings → Runtimes，看到刚注册的 runtime 状态为 **online** 即成功。

如果 runtime 不上线：

1. CLI 模式：`multica daemon status` 看 daemon 是否在跑；不在跑就 `multica daemon start`
2. Desktop 模式：检查 `~/.multica/desktop.json` 内容是不是上面 B.2 那段；改完必须**完全退出**再开
3. 都不行的话，让用户截图 Settings → Runtimes 页面 + daemon 日志 (`~/.multica/logs/`) 找平台维护者。
