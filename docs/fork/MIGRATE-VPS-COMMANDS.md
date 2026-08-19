# Runbook thao tác — migrate `hira2` sang VPS mới

Bản lệnh chạy tay, đi kèm [`MIGRATE-VPS.md`](MIGRATE-VPS.md) (giải thích *vì sao*).
Mỗi bước có **cổng kiểm tra** — không qua cổng thì dừng, đừng chạy bước tiếp theo.

```
VPS cũ  :  hira@72.62.64.42          — giữ chạy lâu dài (v1, kb.thichcay.vn, cqa)
VPS mới :  saucevn@187.127.214.83    — lumi-prod đang giữ 80/443
Laptop  :  làm cầu chuyển file, không cần SSH key giữa hai VPS
R2 acct :  7fd5b98820bc8fbeb478181ffb473048
```

Lệnh nào bắt đầu bằng `ssh hira@…` / `ssh saucevn@…` là **chạy từ laptop**.
Lệnh trong khối có ghi `# trên VPS …` là chạy sau khi đã `ssh` vào máy đó.

> **Chạy các bước dài (§A4 chuyển image, §C3 dump) trong `tmux` hoặc `screen`.**
> Rớt SSH giữa chừng là mất cả quá trình.

---

# PHẦN 0 — Khẩn, làm ngay, độc lập với migrate

## 0.1 Đưa backup ra khỏi bucket public

Thứ tự quan trọng: **đẩy lên chỗ mới trước, xoá chỗ cũ sau.** Không bao giờ để lúc nào đó
không còn bản backup nào.

```bash
ssh hira@72.62.64.42 'cp ~/.config/rclone/rclone.conf ~/.config/rclone/rclone.conf.bak && grep -n endpoint ~/.config/rclone/rclone.conf'
```

Sửa `endpoint` — bỏ phần path `/hira-uploads` ở cuối:

```bash
ssh hira@72.62.64.42 "sed -i -E 's#^(endpoint = https://[^/]+)/hira-uploads/?\$#\\1#' ~/.config/rclone/rclone.conf && grep -n endpoint ~/.config/rclone/rclone.conf"
```

**Cổng kiểm tra:** dòng `endpoint` giờ phải kết thúc bằng `.r2.cloudflarestorage.com`, không có
gì phía sau.

Tạo bucket private và đẩy backup lên đó:

```bash
ssh hira@72.62.64.42 'rclone mkdir r2:hira-backups && bash ~/bin/backup.sh && rclone ls r2:hira-backups'
```

**Cổng kiểm tra:** phải thấy 1 file `.sql.gz` mới trong `r2:hira-backups`.

Giờ mới xoá bản public:

```bash
ssh hira@72.62.64.42 'rclone purge r2:hira-uploads/hira-backups'
```

```bash
curl -sI https://files.hira.vn/hira-backups/hira-db-20260819-030001.sql.gz | head -1
```

**Cổng kiểm tra:** phải là `HTTP/2 404`. Nếu vẫn 200 → CDN đang cache, purge cache ở Cloudflare
dashboard rồi kiểm tra lại. Chưa 404 thì chưa xong.

## 0.2 Bind loopback cho 3 port của stack v1

⚠️ Bước này **recreate container v1** → `app.hira.vn` gián đoạn ~30 giây. Chọn giờ vắng.

```bash
ssh hira@72.62.64.42 'cd /home/hira/hira && cp docker-compose.selfhost.yml docker-compose.selfhost.yml.bak && grep -nE "^\s+- \"[0-9]+:[0-9]+\"" docker-compose.selfhost.yml'
```

Xem output, xác nhận đúng 3 dòng `"8080:8080"`, `"3000:3000"`, `"5432:5432"` rồi mới chạy:

```bash
ssh hira@72.62.64.42 'cd /home/hira/hira && sed -i -E "s#^(\s+)- \"([0-9]+):([0-9]+)\"#\1- \"127.0.0.1:\2:\3\"#" docker-compose.selfhost.yml && git diff --no-index docker-compose.selfhost.yml.bak docker-compose.selfhost.yml | head -30'
```

```bash
ssh hira@72.62.64.42 'cd /home/hira/hira && docker compose -p multica -f docker-compose.selfhost.yml up -d && docker ps --format "{{.Names}}\t{{.Ports}}" | grep multica'
```

**Cổng kiểm tra** — từ laptop, cả ba phải **timeout / refused**:

```bash
for p in 5432 8080 3000; do nc -z -w3 72.62.64.42 $p && echo "$p VẪN MỞ 🔴" || echo "$p đã đóng ✅"; done
```

Và `https://app.hira.vn` phải trở lại bình thường.

## 0.3 Xoá cron chứa token Meta

```bash
ssh hira@72.62.64.42 'crontab -l > ~/crontab.bak.$(date +%F) && crontab -l | grep -c META_ACCESS_TOKEN'
```

```bash
ssh -t hira@72.62.64.42 'crontab -e'
```

Xoá dòng bắt đầu bằng `0 17 28 4 *`. Sau đó xoay token đó trong Meta Business.

---

# PHẦN A — Chuẩn bị (không ảnh hưởng gì đang chạy)

## A1. Backup `hira2` — thứ chưa từng được backup

```bash
ssh hira@72.62.64.42 "docker exec hira2-postgres-1 pg_dump -U multica -d multica -Fc" > ~/hira2-dryrun-$(date +%F).dump && ls -lh ~/hira2-dryrun-*.dump
```

**Cổng kiểm tra:** file phải **> 10 MB**. Nếu chỉ vài KB là dump lỗi — đọc lại output, đừng đi tiếp.

```bash
pg_restore -l ~/hira2-dryrun-$(date +%F).dump | head -5
```

(Không có `pg_restore` trên laptop thì bỏ qua bước này, sẽ kiểm ở §A8.)

## A2. Dọn nợ ACME trên VPS mới

```bash
ssh saucevn@187.127.214.83 'cp /srv/lumi/Caddyfile /srv/lumi/Caddyfile.bak.$(date +%F-%H%M) && ls -la /srv/lumi/Caddyfile.bak.*'
```

```bash
ssh saucevn@187.127.214.83 "sed -i -E '/^(lms\.thichcay\.vn|lumi\.hira\.vn)[[:space:]]/s/^/# /' /srv/lumi/Caddyfile && grep -n 'thichcay\|hira.vn\|bebe.group' /srv/lumi/Caddyfile"
```

**Cổng kiểm tra:** 2 dòng `lms.thichcay.vn` và `lumi.hira.vn` phải có `# ` ở đầu; 2 dòng
`lumi.bebe.group` và `lumi.thichcay.vn` **không được** đụng tới.

```bash
ssh saucevn@187.127.214.83 'docker exec lumi-prod-caddy-1 caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile'
```

Chỉ khi validate OK:

```bash
ssh saucevn@187.127.214.83 'docker exec -w /etc/caddy lumi-prod-caddy-1 caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile'
```

**Cổng kiểm tra** — bắt buộc, ngay lập tức:

```bash
for d in lumi.thichcay.vn lumi.bebe.group; do echo -n "$d: "; curl -sI https://$d | head -1; done
```

Cả hai phải `HTTP/2 200`. Nếu không → `ssh saucevn@187.127.214.83 'cp /srv/lumi/Caddyfile.bak.* /srv/lumi/Caddyfile'` rồi reload lại.

Sau 15 phút, xác nhận đã hết lỗi ACME:

```bash
ssh saucevn@187.127.214.83 'docker logs lumi-prod-caddy-1 --since 15m 2>&1 | grep -ci "challenge failed\|could not get certificate"'
```

**Cổng kiểm tra:** phải là `0`.

## A3. Tạo swap 4 GB trên VPS mới

```bash
ssh -t saucevn@187.127.214.83 'sudo fallocate -l 4G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile && echo "/swapfile none swap sw 0 0" | sudo tee -a /etc/fstab && sudo sysctl -w vm.swappiness=10 && free -h'
```

**Cổng kiểm tra:** dòng `Swap:` phải hiện `4.0Gi`.

## A4. Chuyển 2 image sang VPS mới

Kiểm tra kiến trúc trước — khác kiến trúc là image vô dụng:

```bash
ssh hira@72.62.64.42 "docker inspect multica-backend:dev multica-web:dev --format '{{.Os}}/{{.Architecture}}'"; ssh saucevn@187.127.214.83 "docker version --format '{{.Server.Os}}/{{.Server.Arch}}'"
```

**Cổng kiểm tra:** cả ba dòng đều `linux/amd64`.

Chuyển (chạy trong `tmux`, mất khá lâu — image đi qua laptop hai chiều):

```bash
ssh hira@72.62.64.42 "docker save multica-backend:dev multica-web:dev | gzip -1" | ssh saucevn@187.127.214.83 "gunzip | docker load"
```

**Cổng kiểm tra:**

```bash
ssh saucevn@187.127.214.83 'docker images | grep -E "multica-(backend|web)"'
```

Phải thấy đúng 2 image `:dev`.

## A5. `.env` cho VPS mới

```bash
ssh saucevn@187.127.214.83 'mkdir -p ~/hira2' && scp hira@72.62.64.42:/home/hira/hira-new/.env ~/hira2.env && chmod 600 ~/hira2.env
```

Sinh mật khẩu Postgres mới (cái cũ 7 ký tự):

```bash
openssl rand -base64 32 | tr -dc 'A-Za-z0-9' | head -c 32; echo
```

Sửa `~/hira2.env` trên laptop — **4 thay đổi, không hơn**:

| | |
|---|---|
| `POSTGRES_PASSWORD=` | dán mật khẩu vừa sinh |
| `DATABASE_URL=` | **xoá cả dòng** (compose set đè, để lại chỉ gây nhầm) |
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GOOGLE_AI_KEY` | **xoá cả 3 dòng** (chỉ phục vụ daemon, mà daemon đã bỏ) |
| `JWT_SECRET=` | **GIỮ NGUYÊN** — đổi là logout toàn bộ user + chết 36 personal access token |

```bash
scp ~/hira2.env saucevn@187.127.214.83:~/hira2/.env && ssh saucevn@187.127.214.83 'chmod 600 ~/hira2/.env && grep -c . ~/hira2/.env'
```

**Cổng kiểm tra:**

```bash
ssh saucevn@187.127.214.83 'grep -E "^(JWT_SECRET|POSTGRES_PASSWORD|MULTICA_APP_URL|FRONTEND_ORIGIN|S3_BUCKET)=" ~/hira2/.env | sed -E "s/=(.{6}).*/=\1…/"'
```

`MULTICA_APP_URL` và `FRONTEND_ORIGIN` phải là `https://app2.hira.vn`, `S3_BUCKET=hira-uploads`.

## A6. Đưa file deploy lên VPS mới

Từ thư mục repo trên laptop:

```bash
scp deploy/hira2/docker-compose.vps.yml deploy/hira2/Caddyfile.hira2 saucevn@187.127.214.83:~/hira2/
```

## A7. Copy cert TLS của `app2.hira.vn`

```bash
ssh -t hira@72.62.64.42 'sudo find /var/lib/caddy -path "*app2.hira.vn*" -name "*.crt" -o -path "*app2.hira.vn*" -name "*.key"'
```

Thay `<DIR>` bằng thư mục vừa tìm được:

```bash
ssh -t hira@72.62.64.42 'sudo cp <DIR>/app2.hira.vn.crt <DIR>/app2.hira.vn.key /tmp/ && sudo chown hira: /tmp/app2.hira.vn.* && openssl x509 -enddate -noout -in /tmp/app2.hira.vn.crt'
```

**Cổng kiểm tra:** `notAfter` phải còn ít nhất ~30 ngày. Ít hơn thì đợi Caddy cũ gia hạn xong
rồi copy lại — cert này phải sống qua được cả giai đoạn staging lẫn cutover.

```bash
scp hira@72.62.64.42:/tmp/app2.hira.vn.{crt,key} ~/ && scp ~/app2.hira.vn.{crt,key} saucevn@187.127.214.83:~/hira2/
```

```bash
ssh saucevn@187.127.214.83 'docker exec lumi-prod-caddy-1 mkdir -p /data/hira2 && docker cp ~/hira2/app2.hira.vn.crt lumi-prod-caddy-1:/data/hira2/ && docker cp ~/hira2/app2.hira.vn.key lumi-prod-caddy-1:/data/hira2/ && docker exec lumi-prod-caddy-1 ls -l /data/hira2/'
```

Dọn bản tạm:

```bash
ssh hira@72.62.64.42 'rm -f /tmp/app2.hira.vn.crt /tmp/app2.hira.vn.key'; rm -f ~/app2.hira.vn.crt ~/app2.hira.vn.key
```

## A8. Dựng stack + restore thử

```bash
scp ~/hira2-dryrun-$(date +%F).dump saucevn@187.127.214.83:~/hira2/hira2.dump
```

```bash
ssh saucevn@187.127.214.83 'cd ~/hira2 && docker compose -f docker-compose.vps.yml up -d && sleep 20 && docker compose -f docker-compose.vps.yml ps'
```

**Cổng kiểm tra:** 3 container `hira2-hira-db-1`, `hira2-hira-api-1`, `hira2-hira-web-1` đều Up.

```bash
ssh saucevn@187.127.214.83 'curl -s 127.0.0.1:8081/health; echo; curl -sI 127.0.0.1:3001 | head -1'
```

Restore vào DB rỗng — sạch hơn `--clean` đè lên schema vừa migrate:

```bash
ssh saucevn@187.127.214.83 'cd ~/hira2 && docker compose -f docker-compose.vps.yml stop hira-api && docker exec hira2-hira-db-1 psql -U multica -d postgres -c "DROP DATABASE multica WITH (FORCE);" -c "CREATE DATABASE multica OWNER multica;"'
```

```bash
ssh saucevn@187.127.214.83 'docker exec -i hira2-hira-db-1 pg_restore -U multica -d multica --no-owner < ~/hira2/hira2.dump; echo "exit=$?"'
```

```bash
ssh saucevn@187.127.214.83 'cd ~/hira2 && docker compose -f docker-compose.vps.yml start hira-api && sleep 10 && curl -s 127.0.0.1:8081/health'
```

**Cổng kiểm tra — đối chiếu số liệu:**

```bash
ssh saucevn@187.127.214.83 'docker exec hira2-hira-db-1 psql -U multica -d multica -At -F" | " -c "select (select count(*) from schema_migrations) mig, (select count(*) from \"user\") usr, (select count(*) from workspace) ws, (select count(*) from issue) iss, (select count(*) from comment) cmt, (select count(*) from attachment) att;"'
```

Kỳ vọng (số đo ngày 20/08): `152 | 20 | 15 | 469 | 1104 | 67`. Lệch nhỏ ở `issue`/`comment` là
bình thường nếu có ghi mới; lệch ở `schema_migrations` thì **dừng lại** — sai image.

## A9. Nối Caddy

Kiểm tra Caddy nhìn thấy hira2 **trước khi** đụng config:

```bash
ssh saucevn@187.127.214.83 'docker exec lumi-prod-caddy-1 wget -q -O /dev/null --timeout=5 http://hira-web:3000/ && echo "hira-web OK"; docker exec lumi-prod-caddy-1 wget -q -O /dev/null --timeout=5 http://hira-api:8080/health && echo "hira-api OK"'
```

**Cổng an toàn quan trọng nhất — alias của lumi phải KHÔNG bị nhiễm:**

```bash
ssh saucevn@187.127.214.83 'for c in hira2-hira-web-1 hira2-hira-api-1 hira2-hira-db-1; do echo "== $c"; docker inspect $c --format "{{range \$n, \$v := .NetworkSettings.Networks}}{{\$n}} -> aliases {{\$v.Aliases}}{{println}}{{end}}"; done'
```

Đọc kỹ output — đây là điều kiện đúng:

- `hira-web` / `hira-api`: có mặt trên **cả hai** network. Trên `lumi-prod_default` alias
  **chỉ được** là `hira-web`/`hira-api`. Alias `backend` **chỉ** được xuất hiện ở network
  `hira2_internal`.
- `hira-db`: **chỉ** ở `hira2_internal`, alias `postgres` nằm ở đó. Nếu nó xuất hiện trên
  `lumi-prod_default` là sai.

Xác nhận lần hai từ góc nhìn của chính Caddy:

```bash
ssh saucevn@187.127.214.83 'docker exec lumi-prod-caddy-1 nslookup frontend 2>/dev/null | grep -c "^Address" ; docker exec lumi-prod-caddy-1 nslookup postgres 2>/dev/null | grep -c "^Address"'
```

Mỗi lệnh trả về `2` (1 dòng địa chỉ của DNS server + 1 dòng kết quả) = **đúng một** địa chỉ cho
mỗi tên. Ra `3` nghĩa là alias bị nhiễm → `docker compose -f docker-compose.vps.yml down` ngay
và đọc lại §2.2 trong `MIGRATE-VPS.md`.

Giờ mới thêm site block:

```bash
ssh saucevn@187.127.214.83 'cp /srv/lumi/Caddyfile /srv/lumi/Caddyfile.bak.$(date +%F-%H%M) && cat ~/hira2/Caddyfile.hira2 >> /srv/lumi/Caddyfile && tail -20 /srv/lumi/Caddyfile'
```

```bash
ssh saucevn@187.127.214.83 'docker exec lumi-prod-caddy-1 caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile'
```

Chỉ khi validate OK:

```bash
ssh saucevn@187.127.214.83 'docker exec -w /etc/caddy lumi-prod-caddy-1 caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile'
```

**Cổng kiểm tra — lumi-prod phải còn nguyên:**

```bash
for d in lumi.thichcay.vn lumi.bebe.group; do echo -n "$d: "; curl -sI https://$d | head -1; done
```

Và hira2 đã phục vụ được (chưa đổi DNS, gọi thẳng IP + SNI):

```bash
curl -sI --resolve app2.hira.vn:443:187.127.214.83 https://app2.hira.vn | head -3
```

**Cổng kiểm tra:** `HTTP/2 200`. Nếu TLS alert → cert chưa vào đúng chỗ, xem lại §A7.

**Rollback bước này:** `ssh saucevn@187.127.214.83 'cp /srv/lumi/Caddyfile.bak.<timestamp> /srv/lumi/Caddyfile'` rồi reload lại. Không bao giờ cần `docker compose up` cho lumi.

---

# PHẦN B — Verify staging bằng `/etc/hosts` (chưa đụng DNS)

```bash
sudo sh -c 'echo "187.127.214.83 app2.hira.vn" >> /etc/hosts' && dscacheutil -flushcache; sudo killall -HUP mDNSResponder
```

```bash
curl -sI https://app2.hira.vn | head -3; curl -s https://app2.hira.vn/health; echo
```

Mở trình duyệt `https://app2.hira.vn` và kiểm tra **đủ 6 mục**:

- [ ] Login bằng email + mã Resend
- [ ] Workspace / issue / comment hiện đủ, khớp số ở §A8
- [ ] Mở 1 attachment (tải từ `files.hira.vn`)
- [ ] UI tiếng Việt + màu brand Hira → đúng image fork, không phải image upstream
- [ ] **Realtime**: mở 2 tab cùng 1 issue, comment ở tab này hiện ngay ở tab kia
- [ ] Tạo 1 issue mới rồi xoá → xác nhận ghi được xuống DB mới

> Chrome bật "Secure DNS" sẽ bỏ qua `/etc/hosts`. Nếu trang không load, tắt nó ở
> `chrome://settings/security`, hoặc test bằng Safari.

```bash
ssh saucevn@187.127.214.83 'docker logs lumi-prod-caddy-1 --since 20m 2>&1 | grep -ci "challenge failed"'
```

**Cổng kiểm tra:** `0` — dòng `tls` trong site block phải khiến Caddy **không** gọi ACME.

Xoá hosts entry sau khi xong:

```bash
sudo sed -i '' '/187\.127\.214\.83 app2\.hira\.vn/d' /etc/hosts && grep -c app2.hira.vn /etc/hosts
```

---

# PHẦN C — Cutover (~15 phút)

Từ đây có downtime. Thông báo trước.

**C1.** Dừng ghi ở VPS cũ (giữ postgres để dump):

```bash
ssh hira@72.62.64.42 'cd /home/hira/hira-new && docker compose -p hira2 -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f docker-compose.migrate.yml stop backend frontend && docker ps --format "{{.Names}}" | grep hira2'
```

**Cổng kiểm tra:** chỉ còn `hira2-postgres-1`.

**C2.** Dump cuối + chuyển sang:

```bash
ssh hira@72.62.64.42 "docker exec hira2-postgres-1 pg_dump -U multica -d multica -Fc" > ~/hira2-final.dump && ls -lh ~/hira2-final.dump && scp ~/hira2-final.dump saucevn@187.127.214.83:~/hira2/hira2.dump
```

**C3.** Restore đè:

```bash
ssh saucevn@187.127.214.83 'cd ~/hira2 && docker compose -f docker-compose.vps.yml stop hira-api && docker exec hira2-hira-db-1 psql -U multica -d postgres -c "DROP DATABASE multica WITH (FORCE);" -c "CREATE DATABASE multica OWNER multica;" && docker exec -i hira2-hira-db-1 pg_restore -U multica -d multica --no-owner < ~/hira2/hira2.dump; docker compose -f docker-compose.vps.yml start hira-api'
```

```bash
ssh saucevn@187.127.214.83 'sleep 10; curl -s 127.0.0.1:8081/health; echo; docker exec hira2-hira-db-1 psql -U multica -d multica -At -c "select count(*) from issue;"'
```

**C4.** Đổi DNS — **thủ công trong Cloudflare dashboard**, đủ **4 record ở 2 zone**, đổi giá trị
A record từ `72.62.64.42` → `187.127.214.83`:

| Zone | Record |
|---|---|
| `hira.vn` | `app2.hira.vn` |
| `hira.vn` | `test.hira.vn` |
| `bebe.group` | `multica.bebe.group` |
| `bebe.group` | `hira.bebe.group` |

Giữ nguyên trạng thái proxy (🟠) như đang có. Vì proxied nên hiệu lực gần như tức thì, không
phải chờ TTL.

**C5.** Verify:

```bash
for d in app2.hira.vn test.hira.vn multica.bebe.group hira.bebe.group; do echo -n "$d: "; curl -sI https://$d | head -1; done
```

```bash
for d in lumi.thichcay.vn lumi.bebe.group app.hira.vn; do echo -n "$d: "; curl -sI https://$d | head -1; done
```

Rồi mở trình duyệt, chạy lại **đủ 6 mục** của Phần B trên `https://app2.hira.vn` thật.

**C6.** Dừng hẳn stack cũ + tắt daemon v2:

```bash
ssh hira@72.62.64.42 'pgrep -af "multica daemon"'
```

Phải thấy đúng 2 dòng. Chỉ giết dòng có `--profile v2`:

```bash
ssh hira@72.62.64.42 'pkill -f "multica daemon start --foreground --profile v2"; sleep 2; pgrep -af "multica daemon"'
```

**Cổng kiểm tra:** còn đúng **1** tiến trình (cái phục vụ v1) — giết nhầm là `app.hira.vn` mất agent.

```bash
ssh hira@72.62.64.42 'cd /home/hira/hira-new && docker compose -p hira2 -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f docker-compose.migrate.yml stop && docker ps --format "{{.Names}}"'
```

**KHÔNG** chạy `down`, **KHÔNG** xoá volume — đó là cửa sổ rollback.

## Rollback (bất kỳ lúc nào sau C4)

1. Đổi 4 A record về `72.62.64.42` trong Cloudflare.
2. `ssh hira@72.62.64.42 'cd /home/hira/hira-new && docker compose -p hira2 -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f docker-compose.migrate.yml start'`

DB cũ nguyên vẹn — không có ghi mới nào sau C1. Không cần đụng Caddyfile ở cả hai máy.

---

# PHẦN D — Sau cutover

## D1. Tắt autopilot trigger (trong app)

20 trigger vẫn nhận webhook và đẩy task vào hàng đợi, nhưng không còn agent runtime nào tiêu
thụ. Vào từng workspace → Autopilot → tắt. Kiểm tra hàng đợi có ngừng phình:

```bash
ssh saucevn@187.127.214.83 'docker exec hira2-hira-db-1 psql -U multica -d multica -At -c "select count(*) from agent_task_queue;"'
```

Chạy lại sau 1 ngày — con số không được tăng.

## D2. Dựng backup cho `hira2` trên VPS mới

```bash
ssh saucevn@187.127.214.83 'sudo apt-get update -qq && sudo apt-get install -y rclone'
```

Copy `rclone.conf` **đã sửa** (§0.1) — copy bản chưa sửa là tái tạo lỗ hổng trên máy mới:

```bash
ssh hira@72.62.64.42 'cat ~/.config/rclone/rclone.conf' | ssh saucevn@187.127.214.83 'mkdir -p ~/.config/rclone && cat > ~/.config/rclone/rclone.conf && chmod 600 ~/.config/rclone/rclone.conf && grep endpoint ~/.config/rclone/rclone.conf'
```

**Cổng kiểm tra:** `endpoint` không được có `/hira-uploads` ở cuối.

```bash
scp deploy/hira2/backup-hira2.sh saucevn@187.127.214.83:~/bin/ && ssh saucevn@187.127.214.83 'mkdir -p ~/backups && chmod +x ~/bin/backup-hira2.sh && ~/bin/backup-hira2.sh && rclone ls r2:hira-backups | grep hira2'
```

```bash
ssh saucevn@187.127.214.83 '(crontab -l 2>/dev/null; echo "15 3 * * * /home/saucevn/bin/backup-hira2.sh >> /home/saucevn/backups/backup.log 2>&1") | crontab - && crontab -l'
```

## D3. Trả TLS về cho ACME (sau khi DNS ổn định vài ngày)

```bash
ssh saucevn@187.127.214.83 'cp /srv/lumi/Caddyfile /srv/lumi/Caddyfile.bak.$(date +%F-%H%M) && sed -i -E "\|^[[:space:]]*tls /data/hira2/|d" /srv/lumi/Caddyfile && grep -c "tls /data/hira2" /srv/lumi/Caddyfile; echo "(0 = da xoa xong)"'
```

```bash
ssh saucevn@187.127.214.83 'docker exec lumi-prod-caddy-1 caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile && docker exec -w /etc/caddy lumi-prod-caddy-1 caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile'
```

```bash
ssh saucevn@187.127.214.83 'sleep 60; docker logs lumi-prod-caddy-1 --since 3m 2>&1 | grep -i "certificate obtained\|challenge failed" | tail -5'
```

**Cổng kiểm tra:** phải thấy `certificate obtained` cho `app2.hira.vn`, không có `challenge failed`.
Nếu fail → khôi phục file `.bak` và reload; cert copy vẫn dùng được cho tới khi hết hạn.

Sau đó xoá cert thủ công để Caddy quản lý hoàn toàn:

```bash
ssh saucevn@187.127.214.83 'docker exec lumi-prod-caddy-1 rm -rf /data/hira2'
```

## D4. Sửa Caddy VPS cũ (máy này ở lại lâu dài)

```bash
ssh -t hira@72.62.64.42 'sudo mkdir -p /var/log/caddy && sudo chown caddy:caddy /var/log/caddy && sudo systemctl status caddy --no-pager | head -5'
```

So config đang chạy với config trên đĩa **trước khi** reload:

```bash
ssh hira@72.62.64.42 'curl -s localhost:2019/config/ | jq -S . > /tmp/running.json; caddy adapt --config /etc/caddy/Caddyfile 2>/dev/null | jq -S . > /tmp/ondisk.json; diff /tmp/running.json /tmp/ondisk.json | head -40; echo "khác nhau $(diff /tmp/running.json /tmp/ondisk.json | wc -l) dòng"'
```

Nếu khác nhiều thì đọc kỹ diff trước khi reload — reload sẽ nạp bản trên đĩa.

## D5. Xoay secret

⚠️ **v1 vẫn dùng chung `RESEND_API_KEY` và R2 keys.** Mỗi lần xoay phải cập nhật **cả hai**
`.env` rồi restart cả hai stack, nếu không v1 gãy im lặng: không gửi được mã đăng nhập, không
upload được attachment.

Thứ tự đề xuất, mỗi cái một lần riêng, verify xong mới sang cái tiếp:

1. `RESEND_API_KEY` → sửa `/home/hira/hira/.env` (v1) + `~/hira2/.env` (mới) → restart cả hai → thử login ở cả `app.hira.vn` lẫn `app2.hira.vn`.
2. R2 keys (`AWS_ACCESS_KEY_ID`/`SECRET`, `R2_*`) + `rclone.conf` ở cả hai máy → thử upload + tải attachment ở cả hai.
3. `GITHUB_WEBHOOK_SECRET` và `MULTICA_LARK_SECRET_KEY` → **hai giá trị KHÁC NHAU** (hiện đang trùng nhau, cùng sha256).
4. `JWT_SECRET` — **làm sau cùng, thành một lần riêng**. Logout toàn bộ user + vô hiệu 36 PAT. Thông báo trước cho người dùng.

## D6. Hết cửa sổ rollback (≥ 7 ngày)

```bash
ssh hira@72.62.64.42 'cp /etc/caddy/Caddyfile ~/Caddyfile.bak.$(date +%F) && grep -n "app2.hira.vn\|test.hira.vn\|multica.bebe.group\|hira.bebe.group" /etc/caddy/Caddyfile'
```

Xoá 4 site block đó khỏi Caddyfile cũ (chúng trỏ `localhost:3001`/`8081` giờ đã chết), rồi
`sudo caddy validate` + `sudo systemctl reload caddy`. Để lại chỉ là bẫy cho người đọc sau này.

Chỉ khi mọi thứ đã ổn định:

```bash
ssh hira@72.62.64.42 'cd /home/hira/hira-new && docker compose -p hira2 -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f docker-compose.migrate.yml down'
```

**Vẫn KHÔNG dùng `-v`** — giữ volume `hira2_pgdata` thêm một thời gian nữa.
