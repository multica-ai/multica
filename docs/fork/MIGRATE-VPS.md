# Migrate stack `hira2` sang VPS mới (ở chung máy với `lumi-prod`)

Chuyển deployment Hira (`app2.hira.vn`) từ **VPS cũ** `hira@72.62.64.42` sang **VPS mới**
`saucevn@187.127.214.83` — nơi đã có stack `lumi-prod` chạy production, giữ 80/443.

Runbook này đã được viết lại theo **báo cáo khảo sát thực tế ngày 2026-08-20** của cả hai máy.
Mọi con số bên dưới là số đo, không phải giả định.

> Đọc kèm [`MIGRATION.md`](MIGRATION.md) (runbook v1 → fork, đã tạo ra stack `hira2`).
> File đi kèm: [`../../deploy/hira2/docker-compose.vps.yml`](../../deploy/hira2/docker-compose.vps.yml)
> · [`../../deploy/hira2/Caddyfile.hira2`](../../deploy/hira2/Caddyfile.hira2)

---

## 0. Ba việc KHẨN, làm ngay, không chờ migrate

Khảo sát VPS cũ phát hiện 3 lỗ hổng đang mở. Chúng **độc lập với việc migrate** — đừng gộp
vào lịch cutover.

### 0.1 🔴 Dump database production đang tải công khai trên Internet

```
https://files.hira.vn/hira-backups/hira-db-20260819-030001.sql.gz  →  HTTP 200, 19 MB
```

Nguyên nhân: `~/.config/rclone/rclone.conf` khai `endpoint` **kèm path** `/hira-uploads`, nên
`r2:hira-backups` không trỏ tới bucket `hira-backups` mà tới **prefix `hira-backups/` bên trong
bucket public `hira-uploads`** (bucket được map ra CDN `files.hira.vn`). 8 bản dump đang nằm đó,
mỗi bản chứa toàn bộ user, hash mật khẩu, token và nội dung.

Xử lý theo thứ tự:

```bash
rclone --s3-endpoint https://7fd5b98820bc8fbeb478181ffb473048.r2.cloudflarestorage.com \
  purge r2:hira-uploads/hira-backups
```

Rồi sửa `rclone.conf` — bỏ `/hira-uploads` khỏi dòng `endpoint`, tạo bucket `hira-backups`
**private** riêng, chạy lại `backup.sh` một lần và **kiểm chứng bằng `curl -I`** rằng URL công
khai giờ trả 404. Sau khi dump đã bị lộ, phải coi như **mọi secret trong DB đã lộ** → xoay
secret là bắt buộc (§6.4), không phải tuỳ chọn.

### 0.2 🔴 Postgres của stack v1 mở ra `0.0.0.0:5432`, mật khẩu 7 ký tự

Đã xác nhận TCP OPEN từ IP public. Docker chèn iptables rule trước UFW nên firewall host
không chặn. Backend v1 (`:8080`) và frontend v1 (`:3000`) cũng đang `0.0.0.0`.
Sửa `/home/hira/hira/docker-compose.selfhost.yml` → thêm tiền tố `127.0.0.1:` vào cả 3
port mapping, rồi `docker compose up -d`. Stack `hira2` đã bind loopback đúng, không phải sửa.

### 0.3 🟠 Token Meta/Facebook plaintext trong crontab

`crontab -e`, xoá job `0 17 28 4 *` (script đích ở `/tmp` gần như chắc chắn đã bị dọn mất,
chỉ còn lại chỗ rò rỉ token 195 ký tự). Xoay token đó phía Meta.

---

## 1. Hiện trạng đã đo được

### 1.1 VPS cũ — có **hai** stack Hira, không phải một

| | `hira2` (v2, app2.hira.vn) | `multica` (v1, app.hira.vn) |
|---|---|---|
| DB size | 61 MB | 75 MB |
| schema_migrations | 152 | 68 |
| user / workspace | 20 / 15 | 17 / 12 |
| issue / comment | 469 / 1104 | **1311 / 1825** |
| issue mới nhất | 2026-07-07 | **2026-08-19 17:01** |
| comment mới nhất | 2026-07-02 | **2026-08-19 17:12** |
| Port | loopback ✅ | `0.0.0.0` ❌ |
| Backup tự động | **KHÔNG CÓ** | có (cron 03:00) |

> 🔴 **Cutover v1 → hira2 chưa từng hoàn tất.** `app.hira.vn` (v1) mới là nơi người dùng tạo
> issue/comment hằng ngày; `hira2` vẫn sống (cron + agent runtime heartbeat tới 19/08) nhưng
> hoạt động nghiệp vụ đã quay về v1 từ tháng 7. **Migrate chỉ `hira2` rồi tắt VPS cũ = mất
> phần lớn nội dung đang dùng.** Đây là quyết định phạm vi phải chốt trước — xem §8.

Ngoài Hira, VPS cũ còn phục vụ **`kb.thichcay.vn`** (Arkon KB) và stack **`cqa`** của user
`saucevn` (MySQL, `cqa.bebe.group`). Tắt máy cũ là tắt luôn hai thứ này.

### 1.2 `hira2` — những thứ KHÔNG nằm trong git

| Thứ | Vấn đề |
|---|---|
| `multica-backend:dev` / `multica-web:dev` | **build local, không có ở registry nào.** `docker compose pull` trên máy mới sẽ kéo image upstream tiếng Anh, không có Việt hoá. |
| `docker-compose.migrate.yml` | **untracked** — nguồn của binding `127.0.0.1:5434`. `git clone` là mất. |
| Patch `AWS_ACCESS_KEY_ID/SECRET` trong `selfhost.yml` | chưa commit ở commit đang chạy (`65efc411`). **Đã có trên `main` tại `af19eea08`** — không cần copy tay nữa. |
| Project name `hira2` | chỉ đến từ flag `-p hira2` gõ tay; file compose khai `name: multica`. Chạy thiếu flag → volume `multica_pgdata`, DB rỗng, tưởng mất sạch dữ liệu. |
| 2 tiến trình `multica daemon` trên host | không Docker, không systemd, `--foreground` 66 ngày. Reboot là chết. |
| `~/multica_workspaces_v2` (448 MB) | workspace agent trên disk trần, ngoài Docker volume, **chưa từng được backup**. |

Cả ba vấn đề đầu được giải quyết bằng **một file compose self-contained** đã viết sẵn:
[`deploy/hira2/docker-compose.vps.yml`](../../deploy/hira2/docker-compose.vps.yml).

### 1.3 VPS mới

| | |
|---|---|
| RAM / CPU / disk | 7.8 GB (6.2 GB free) · 2 vCPU · 91 GB trống |
| **Swap** | **0 B** — phải tạo trước khi build bất cứ thứ gì |
| Port 8081 / 3001 / 5434 | ✅ trống cả ba |
| Host chỉ mở | 22, 80, 443 |
| Caddyfile | **bind mount `/srv/lumi/Caddyfile`**, owner `saucevn`, mode 664 → sửa được, không cần sudo, không cần recreate container |
| Caddy | `caddy:2-alpine` v2.11.4, **0 plugin DNS** → chỉ HTTP-01 / TLS-ALPN-01, không làm được DNS-01 |
| Admin API | sống ở `127.0.0.1:2019` → `caddy reload` chạy được, graceful |
| Toolchain host | chỉ có Docker + git + jq. **Không node/pnpm/go/make** |
| SSH key | deploy key **chỉ cho repo `saucevn/lumi`**, `IdentitiesOnly yes` → **không clone được `saucevn/multica`** |

🔴 **Nợ ACME đang chảy máu quota:** `lumi.hira.vn` và `lms.thichcay.vn` khai trong Caddyfile
nhưng **chưa có DNS** → Caddy lặp vô hạn xin cert và fail (8 lần/24h), đã bị đẩy xuống LE
staging. Account `admin@thichcay.vn` đang tiến gần giới hạn **5 failed validations/hour** —
rủi ro này chạm tới cả việc **renew cert của 2 domain lumi đang chạy**. Phải dọn trước (§3.2).

---

## 2. Bốn quyết định kiến trúc (đã chốt theo khảo sát)

### 2.1 Image: `docker save/load`, không build tại chỗ — cho lần cutover này

VPS mới đủ RAM về con số nhưng **không có swap** và chỉ 2 vCPU; `next build` chạm trần sẽ gọi
OOM-killer, và nạn nhân có thể là container production của `lumi-prod`. Quan trọng hơn:
`save/load` cho **image bit-identical với cái đang chạy**, loại bỏ hoàn toàn biến số "build lại
ra khác" (lockfile drift, base image đã đổi tag) khỏi một đêm cutover vốn đã nhiều việc.

Vẫn **tạo swap 4 GB trước** — nó rẻ, không chạm `lumi-prod`, và cần cho lần rebuild sau này:

```bash
sudo fallocate -l 4G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
sudo sysctl -w vm.swappiness=10
```

Chuyển sang `deploy = rebuild` (giống `lumi-prod`) **sau khi** hệ thống ổn định — §6.5.

### 2.2 Tên service: `hira-web` / `hira-api` / `hira-db`

Compose gán **tên service làm network alias trên MỌI network** container tham gia.
`lumi-prod` đã có alias `frontend` và `postgres`. Nếu Caddy dùng chung network mà thấy hai
alias `frontend`, embedded DNS trả về 2 địa chỉ A và `reverse_proxy frontend:3000` của lumi
sẽ **round-robin traffic của `lumi.thichcay.vn` sang app Hira**. Đây là kiểu sự cố production
khó chẩn đoán nhất, và override file **không đổi được tên service** — nên phải có file compose
riêng.

Nhưng image lại bake sẵn hai cái tên cũ:

- `multica-web:dev` bake Next.js rewrite → `http://backend:8080` (ARG lúc build, nằm trong
  `routes-manifest.json`, **restart không đổi được**)
- `DATABASE_URL` trỏ host `postgres`

→ Giải bằng **alias theo từng network**: cấp lại `backend` và `postgres` **chỉ trên network
nội bộ**, không lộ ra network dùng chung. `hira-db` không bao giờ rời network nội bộ.

### 2.3 Reverse proxy: thêm site block vào Caddyfile của `lumi-prod`

Ba điều kiện tiên quyết đều đã xác minh: Caddyfile là bind mount sửa được không cần sudo ·
admin API sống · `caddy reload` graceful **và fail-safe** (config mới sai cú pháp thì reload bị
từ chối, config cũ tiếp tục chạy).

`hira-web` + `hira-api` khai `lumi-prod_default` là **external network** ngay trong compose —
tự động, khai báo được, sống sót qua `compose down/up`. Không cần `docker network connect` thủ
công, không chạm container nào của `lumi-prod`.

### 2.4 TLS: copy cert từ VPS cũ, KHÔNG dùng ACME khi cutover

Đây là điểm thay đổi lớn nhất so với dự thảo trước, và nó gỡ được ba nút cùng lúc.

`app2.hira.vn` đang **proxied qua Cloudflare** và origin là VPS cũ. Nếu thêm site block rồi để
Caddy tự xin cert, challenge sẽ đi Cloudflare → VPS cũ → **fail**, và đốt thêm quota vào cái
account vốn đã đang fail (§1.3). Ngược lại, dùng cert copy sẵn (`tls <crt> <key>`) thì:

- Caddy phục vụ được `app2.hira.vn` **trước khi** DNS trỏ về máy mới → verify staging bằng
  `/etc/hosts` trên đúng hostname production, đúng cert, không cần domain tạm.
- Cutover **không có bước xin cert** → không có rủi ro cert, không chạm quota LE.
- Không cần `app2-new.hira.vn`, không cần thêm origin tạm vào `CORS_ALLOWED_ORIGINS`.

Điểm cuối cùng mới là lý do quyết định: `NEXT_PUBLIC_WS_URL=https://app2.hira.vn/ws` **đã bake
vào bundle client**. Trên bất kỳ hostname staging nào, trình duyệt vẫn mở WebSocket về
`app2.hira.vn` → **VPS cũ**. Test trên domain tạm sẽ cho kết quả sai lệch một cách âm thầm;
chỉ `/etc/hosts` mới verify được thật.

Sau cutover, xoá 1 dòng `tls` → Caddy tiếp quản ACME bình thường (§6.3).

---

## 3. Giai đoạn A — Chuẩn bị (không ảnh hưởng gì, làm trước nhiều ngày)

### 3.1 Backup cái chưa từng được backup

```bash
ssh hira@72.62.64.42 "docker exec hira2-postgres-1 pg_dump -U multica -d multica -Fc" > ~/hira2-$(date +%F).dump
```

Lưu vào máy local hoặc storage private — **không phải** `r2:hira-backups` (§0.1).
Bản dump gần nhất của `hira2` là 2026-06-13, đã lỗi thời 2 tháng.

### 3.2 Dọn nợ ACME trên VPS mới (bắt buộc, trước mọi thứ khác)

Trỏ DNS cho `lumi.hira.vn` + `lms.thichcay.vn`, **hoặc** comment 2 block đó khỏi
`/srv/lumi/Caddyfile` rồi reload. Việc này tự nó đã có lợi cho `lumi-prod`.

### 3.3 Tạo swap 4 GB (§2.1) và mở đường git cho repo hira2

`~/.ssh/config` hiện khoá cứng `github.com` vào deploy key của `lumi` (`IdentitiesOnly yes`).
**Thêm block mới**, tuyệt đối không sửa block `Host github.com` đang có:

```
Host github-hira
	HostName github.com
	User git
	IdentityFile ~/.ssh/id_hira_deploy
	IdentitiesOnly yes
```

(Cần cho `git clone` — bản thân lần cutover này không build nên chưa gấp, nhưng cần cho §6.5.)

### 3.4 Chuyển image + code + cấu hình sang

```bash
ssh hira@72.62.64.42 "docker save multica-backend:dev multica-web:dev | gzip -1" \
  | ssh saucevn@187.127.214.83 "gunzip | docker load"
```

Kiểm tra kiến trúc trước khi tin: cả hai máy phải là `linux/amd64`
(`docker inspect <img> --format '{{.Os}}/{{.Architecture}}'`).

Copy `.env` từ `/home/hira/hira-new/.env`. **Giữ nguyên `JWT_SECRET`** — đổi là logout toàn bộ
user và vô hiệu **36 personal access token**. Sửa/thêm:

| Sửa | Vì sao |
|---|---|
| `POSTGRES_PASSWORD` | đang **7 ký tự**; đổi ngay lúc này thì restore tự nhận mật khẩu mới |
| bỏ `DATABASE_URL` khỏi `.env` | không được dùng (compose set đè), chỉ gây nhầm |
| `MULTICA_TRUSTED_PROXIES` | giữ nguyên — `172.16.0.0/12` đã bao subnet của Caddy |
| **bỏ** `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_AI_KEY` | chỉ phục vụ daemon host, mà daemon đã bỏ (§6.1). Compose không map chúng vào container. |

Đặt file compose và site block vào chỗ:

```bash
scp deploy/hira2/docker-compose.vps.yml saucevn@187.127.214.83:~/hira2/
scp deploy/hira2/Caddyfile.hira2        saucevn@187.127.214.83:~/hira2/
```

### 3.5 Copy cert TLS của `app2.hira.vn` từ VPS cũ

VPS cũ chạy Caddy native, cert nằm dưới `/var/lib/caddy/.local/share/caddy/certificates/`.

```bash
sudo find /var/lib/caddy -path '*app2.hira.vn*' -name '*.crt' -o -path '*app2.hira.vn*' -name '*.key'
```

Đưa cặp `.crt`/`.key` vào volume dữ liệu của Caddy trên VPS mới — **`docker cp` ghi thẳng vào
volume, không cần sudo, không restart container**:

```bash
docker exec lumi-prod-caddy-1 mkdir -p /data/hira2
docker cp app2.hira.vn.crt lumi-prod-caddy-1:/data/hira2/
docker cp app2.hira.vn.key lumi-prod-caddy-1:/data/hira2/
```

> Cert LE có hạn 90 ngày — kiểm tra ngày hết hạn (`openssl x509 -enddate -noout -in ...`).
> Đây là cầu tạm cho tới §6.3, không phải giải pháp lâu dài.

### 3.6 Dựng stack trên VPS mới, restore dump thử

```bash
cd ~/hira2 && docker compose -f docker-compose.vps.yml up -d
docker exec -i hira2-hira-db-1 pg_restore -U multica -d multica --clean --if-exists < hira2.dump
docker compose -f docker-compose.vps.yml restart hira-api
curl -s 127.0.0.1:8081/health          # {"status":"ok"}
```

Đối chiếu: `schema_migrations` = 152 · user 20 · workspace 15 · issue 469 · comment 1104 ·
attachment 67.

### 3.7 Nối Caddy — bước rủi ro nhất với `lumi-prod`, làm đúng thứ tự

```bash
cp /srv/lumi/Caddyfile /srv/lumi/Caddyfile.bak.$(date +%F-%H%M)
cat ~/hira2/Caddyfile.hira2 >> /srv/lumi/Caddyfile

docker exec lumi-prod-caddy-1 caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
```

**Chỉ khi validate OK:**

```bash
docker exec -w /etc/caddy lumi-prod-caddy-1 caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

Xác nhận `lumi-prod` không hề hấn — bắt buộc, ngay lập tức:

```bash
curl -sI https://lumi.thichcay.vn | head -1   # HTTP/2 200
curl -sI https://lumi.bebe.group  | head -1   # HTTP/2 200
```

Rollback: `cp /srv/lumi/Caddyfile.bak.* /srv/lumi/Caddyfile` rồi reload lại. **Không bao giờ
cần `docker compose up`.**

---

## 4. Giai đoạn B — Verify staging bằng `/etc/hosts` (chưa đụng DNS)

Trên máy local:

```
187.127.214.83  app2.hira.vn
```

Vì cert đã copy sẵn (§2.4), trình duyệt thấy đúng hostname, đúng cert, đúng WS URL. Test:

- [ ] Login bằng email + mã Resend
- [ ] Duyệt workspace / issue / comment, số lượng khớp bảng §3.6
- [ ] Mở attachment (load từ `files.hira.vn`)
- [ ] UI tiếng Việt + brand Hira (xác nhận đúng image fork, không phải image upstream)
- [ ] **Realtime**: mở 2 tab, comment ở tab này phải hiện ngay ở tab kia (đây là bài test WS —
      nếu domain tạm được dùng thay `/etc/hosts`, bài test này sẽ **giả xanh**)
- [ ] `docker logs lumi-prod-caddy-1 --since 10m` không có lỗi ACME mới

Xoá dòng `/etc/hosts` sau khi xong.

---

## 5. Giai đoạn C — Cutover (~15 phút)

Tất cả domain `hira.vn` đều Cloudflare-proxied → đổi origin là **sửa A record trong dashboard,
hiệu lực gần như tức thì**, không phải chờ TTL 299s.

```
1. Thông báo bảo trì.
2. VPS cũ:  docker compose -p hira2 ... stop backend frontend
            (GIỮ postgres để dump; dừng backend là dừng mọi ghi mới)
3. Dump lần cuối → restore sang VPS mới (lệnh §3.1 + §3.6)
4. VPS mới: docker compose -f docker-compose.vps.yml restart hira-api
5. Cloudflare — đổi A record về 187.127.214.83 cho ĐỦ 4 hostname, ở 2 zone:
      zone hira.vn     : app2.hira.vn , test.hira.vn
      zone bebe.group  : multica.bebe.group , hira.bebe.group
6. curl -sI https://app2.hira.vn   → 200
7. VPS cũ:  docker compose -p hira2 ... stop     (KHÔNG xoá — cửa sổ rollback)
            pkill -f 'multica daemon .* --profile v2'   (§6.1)
            v1 + kb.thichcay.vn + cqa TIẾP TỤC chạy, không đụng tới
```

> ⚠️ Bước 5 — **4 hostname trỏ vào hira2, không phải 1.** Quên 3 cái sau là 3 URL chết.
> `multica.bebe.group` / `hira.bebe.group` ở VPS cũ dùng `tls internal` (self-signed) nên
> Cloudflare SSL mode của zone `bebe.group` đang phải là **Full**, không phải Full (strict).
> Site block mới dùng cert thật → sau cutover có thể nâng zone đó lên **Full (strict)**.
> Nếu chưa nâng mà cứ để Full thì vẫn chạy — đừng đổi 2 thứ cùng lúc.

**Verify sau cutover:** login được (session cũ phải còn sống vì giữ `JWT_SECRET`) · số lượng
issue/comment khớp trước–sau · mở 1 attachment · realtime 2 tab · `lumi.thichcay.vn` +
`lumi.bebe.group` vẫn 200.

**Rollback:** đổi A record về `72.62.64.42` + `docker compose ... start` ở VPS cũ. DB cũ
nguyên vẹn, không có ghi mới nào sau bước 2. Không cần đụng Caddyfile.

---

## 6. Giai đoạn D — Sau cutover

### 6.1 Agent runtime — tắt hẳn, có chủ đích

Quyết định: **không dựng lại agent runtime trên VPS mới.** Việc cần làm là tắt cho gọn, không
phải bỏ mặc — daemon cũ trỏ `server_url=http://localhost:8081` của VPS cũ, sau cutover nó sẽ
quay vòng lỗi kết nối và tiếp tục bơm vào file log vốn đã 1.02 GB.

```bash
# VPS cũ — tắt đúng tiến trình profile v2, GIỮ tiến trình v1 (v1 vẫn đang chạy)
pkill -f 'multica daemon start --foreground --profile v2'
```

Không tắt tiến trình daemon mặc định (PID phục vụ v1) — nó vẫn cần cho `app.hira.vn`.

Hệ quả nhìn thấy được, cần biết trước:

- **25 `agent_runtime`** hiển thị offline trong UI; issue assign cho agent sẽ không ai chạy.
- **20 `autopilot_trigger`** vẫn nhận webhook và vẫn đẩy task vào `agent_task_queue`, nhưng
  không có runtime nào tiêu thụ → hàng đợi phình dần. **Nên tắt các trigger đó trong app**
  sau cutover, thay vì để chúng tích lặng lẽ.
- `~/multica_workspaces_v2` (448 MB) và binary `/usr/local/bin/multica` **không cần mang sang**.
- VPS mới **không cần** node/npm/`claude`/`codex` CLI. Đây là lý do quyết định này gỡ được cả
  một workstream khỏi lịch cutover.
- `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GOOGLE_AI_KEY` trong `.env` chỉ phục vụ daemon host
  (compose không map chúng vào container backend) → **bỏ hẳn khỏi `.env` của VPS mới**. Bớt 3
  secret khỏi một file vừa bị coi là đã lộ.

`sys_cron_executions` là cron nội bộ của backend, chạy trong container — **không liên quan
daemon**, vẫn hoạt động bình thường.

### 6.2 Backup — dựng mới trên VPS mới, không phải sửa cái cũ

`~/bin/backup.sh` ở VPS cũ dump `multica-postgres-1` (v1) — **cứ để nguyên**, v1 ở lại đó và
vẫn cần nó. Cái thiếu là backup cho `hira2`, và giờ nó nằm ở máy khác nên phải dựng mới trên
VPS mới:

- `pg_dump` từ `hira2-hira-db-1`, cron 03:00 (lệch giờ với cron của lumi-prod nếu có).
- Đích là bucket R2 **private** đã tạo ở §0.1 — **không** phải `hira-uploads`.
- Copy `~/.config/rclone/rclone.conf` sang VPS mới với `endpoint` **đã bỏ path** `/hira-uploads`
  (nếu copy y nguyên file cũ là tái tạo lại đúng lỗ hổng §0.1 trên máy mới).
- Kiểm chứng `RETENTION_DAYS=30` chạy đúng — ở VPS cũ nó đang giữ 8 ngày, cũng do lỗi path đó.

Không cần backup `~/multica_workspaces_v2` nữa (§6.1 đã bỏ agent runtime).

Ngày đầu tiên sau cutover: chạy tay một lần, rồi `curl -I` URL công khai tương ứng để chắc
chắn dump **không** tải được từ Internet.

### 6.3 Trả TLS về cho ACME

Sau khi DNS đã ổn định vài ngày: xoá **đúng 1 dòng** `tls /data/hira2/...` khỏi site block →
validate → reload → theo dõi `docker logs -f lumi-prod-caddy-1` cho tới khi thấy cert được cấp.
Bước này bắt buộc, nếu không cert copy sẽ hết hạn sau tối đa 90 ngày và không ai gia hạn.

### 6.4 Xoay secret (bắt buộc, hệ quả của §0.1)

Dump đã public → coi như đã lộ: `RESEND_API_KEY`, R2 keys, các AI key, `GITHUB_WEBHOOK_SECRET`,
`MULTICA_LARK_SECRET_KEY`. Lưu ý hai chỗ:

- `GITHUB_WEBHOOK_SECRET` và `MULTICA_LARK_SECRET_KEY` **đang dùng chung một giá trị**
  (cùng sha256, cùng độ dài 44) — gần như chắc chắn là lỗi copy-paste. Xoay thành **hai giá trị
  khác nhau**.
- **`JWT_SECRET` xoay sau cùng, thành một lần riêng biệt.** Nó logout toàn bộ user và vô hiệu
  36 PAT — đừng trộn vào đêm cutover, sẽ không phân biệt được "lỗi migrate" với "hết session".

⚠️ **v1 ở lại VPS cũ nên xoay secret giờ chạm HAI deployment.** `RESEND_API_KEY` và R2 keys
dùng chung giữa v1 và hira2 (cùng bucket `hira-uploads`). Xoay mà chỉ cập nhật `.env` của hira2
thì **v1 gãy im lặng**: không gửi được mã đăng nhập, không upload được attachment. Mỗi lần xoay
phải cập nhật cả `/home/hira/hira/.env` (v1) lẫn `.env` của hira2 trên VPS mới, rồi restart
cả hai. `JWT_SECRET` thì độc lập từng stack — xoay bên nào chỉ logout người dùng bên đó.

**Lark**: 9 installation + 11 user binding đang hoạt động, callback URL đúc từ
`MULTICA_PUBLIC_URL`. Hostname không đổi → không cần cập nhật gì phía Feishu/Lark console.
Đây chính là lý do nên giữ nguyên `app2.hira.vn` thay vì nhân dịp đổi domain.

### 6.5 Chuyển sang `deploy = rebuild`

Khi đã ổn định: clone repo (§3.3), build tại chỗ từ `main` (đã có sẵn patch R2 credentials tại
`af19eea08`), so sánh với image đang chạy, rồi đổi `HIRA_*_IMAGE` trong `.env`. Từ đó về sau
mỗi lần cập nhật: `sync-upstream.sh` → dịch chuỗi `vi` mới → rebuild → redeploy.

---

## 7. Phạm vi đã chốt — và hệ quả

| Quyết định | Hệ quả với runbook này |
|---|---|
| **Chỉ migrate `hira2`. v1 ở lại VPS cũ.** | Không ETL, không đụng `app.hira.vn`/`api.hira.vn`. Nhưng hai stack tiếp tục **dùng chung bucket R2 `hira-uploads` và chung `RESEND_API_KEY`** → xoay secret phải làm ở cả hai máy (§6.4). |
| **VPS cũ giữ chạy lâu dài** (`kb.thichcay.vn`, `cqa`, v1) | Không cần migrate gì thêm, và cửa sổ rollback không bị giới hạn 7 ngày. Đổi lại, §0.2 (bind loopback cho v1) **không còn là việc dọn dẹp mà là việc bắt buộc** — máy này sẽ phơi Postgres ra Internet vĩnh viễn nếu không sửa. |
| **Bỏ hẳn agent runtime** | Gỡ trọn một workstream khỏi cutover: không mang binary/workspace 448 MB, không cài node/`claude`/`codex` trên VPS mới, bỏ 3 AI key khỏi `.env`. Đổi lại phải tắt chủ động + tắt autopilot trigger (§6.1). |

### Việc thừa hưởng vì VPS cũ sống lâu dài

Caddy trên VPS cũ **đang ở trạng thái reload lỗi**:
`open /var/log/caddy/kb-thichcay-access.log: permission denied`. Nghĩa là config trong RAM có
thể đã lệch với `/etc/caddy/Caddyfile` (file sửa lần cuối 27/07). Khi máy này chỉ còn là trạm
trung chuyển thì bỏ qua được; khi nó ở lại vĩnh viễn thì **lần reload kế tiếp vì bất kỳ lý do
gì cũng có thể nạp một config khác với cái đang chạy**. Sửa sớm:

```bash
sudo mkdir -p /var/log/caddy && sudo chown caddy:caddy /var/log/caddy
# So config đang chạy trong RAM với config trên đĩa TRƯỚC khi reload
curl -s localhost:2019/config/ | jq -S . > /tmp/caddy-running.json
caddy adapt --config /etc/caddy/Caddyfile | jq -S . > /tmp/caddy-ondisk.json
diff /tmp/caddy-running.json /tmp/caddy-ondisk.json

sudo caddy validate --config /etc/caddy/Caddyfile && sudo systemctl reload caddy
```

Sau khi hết cửa sổ rollback, xoá 4 site block trỏ `localhost:3001`/`localhost:8081`
(`app2.hira.vn`, `test.hira.vn`, `multica.bebe.group`, `hira.bebe.group`) khỏi Caddyfile cũ —
để lại thì chúng chỉ là bẫy cho người đọc config sau này.

## 8. Checklist

**Khẩn — không chờ migrate**
- [ ] Purge dump khỏi bucket public + sửa `rclone.conf` + tạo bucket private, verify `curl -I` → 404
- [ ] Bind loopback cho 3 port của stack v1
- [ ] Xoá cron chứa token Meta, xoay token đó

**Giai đoạn A**
- [ ] `pg_dump` hira2 về nơi an toàn (bản gần nhất đã 2 tháng tuổi)
- [ ] Dọn nợ ACME (`lumi.hira.vn`, `lms.thichcay.vn`)
- [ ] Swap 4 GB + `vm.swappiness=10`
- [ ] `docker save/load` 2 image, xác nhận cùng `linux/amd64`
- [ ] Copy `.env` (giữ `JWT_SECRET`, đổi `POSTGRES_PASSWORD`, bỏ `DATABASE_URL`)
- [ ] Copy cert `app2.hira.vn` vào `/data/hira2/` bằng `docker cp`
- [ ] `up -d` → restore → `/health` OK → row count khớp
- [ ] Backup Caddyfile → append block → **`caddy validate`** → reload → `lumi.thichcay.vn` 200

**Giai đoạn B**
- [ ] Verify đủ 6 mục §4 bằng `/etc/hosts` (đặc biệt: realtime WS)

**Giai đoạn C**
- [ ] Cutover §5, đổi **4 record ở 2 zone**
- [ ] Verify sau cutover, `lumi-prod` vẫn xanh
- [ ] Stack `hira2` ở VPS cũ stopped (v1 + kb + cqa vẫn chạy)

**Giai đoạn D**
- [ ] `pkill` daemon profile v2 (KHÔNG tắt daemon v1) + tắt 20 autopilot trigger trong app
- [ ] Backup cho hira2 trên VPS mới: dump `hira2-hira-db-1` → bucket **private**
- [ ] Sửa `/var/log/caddy` owner trên VPS cũ, đối chiếu config RAM vs đĩa
- [ ] Xoá dòng `tls`, trả TLS về ACME, xác nhận cert mới được cấp
- [ ] Xoay secret — **cập nhật cả `.env` của v1 trên VPS cũ**, JWT_SECRET tách riêng làm sau cùng
- [ ] Hết cửa sổ rollback: xoá 4 site block hira2 khỏi Caddyfile của VPS cũ
