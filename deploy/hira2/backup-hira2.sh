#!/bin/bash
# Backup DB của stack hira2 → R2 bucket PRIVATE.
#
# Đích là bucket `hira-backups` riêng, KHÔNG phải `hira-uploads`. Bucket uploads được map ra
# CDN files.hira.vn nên mọi thứ ném vào đó là public — đó chính là cách bản dump production
# từng bị tải tự do trên Internet. Điều kiện: `endpoint` trong rclone.conf KHÔNG kèm path.
#
# Cài: scp lên ~/bin/, chmod +x, rồi thêm cron:
#   15 3 * * * /home/saucevn/bin/backup-hira2.sh >> /home/saucevn/backups/backup.log 2>&1
set -euo pipefail

BACKUP_DIR="$HOME/backups"
CONTAINER="hira2-hira-db-1"
REMOTE="r2:hira-backups"
FILE="$BACKUP_DIR/hira2-db-$(date +%Y%m%d-%H%M%S).sql.gz"

mkdir -p "$BACKUP_DIR"

docker exec "$CONTAINER" pg_dump -U multica -d multica --clean --if-exists | gzip -9 > "$FILE"

# Dump lỗi vẫn tạo ra file gzip hợp lệ nhưng rỗng ruột — bắt trường hợp đó tại đây,
# đừng để nó âm thầm đẩy lên rồi xoá mất bản tốt theo retention.
SIZE=$(stat -c %s "$FILE")
if [ "$SIZE" -lt 1000000 ]; then
	echo "$(date -Is) ABORT: dump chỉ $SIZE bytes, quá nhỏ — giữ nguyên bản cũ" >&2
	rm -f "$FILE"
	exit 1
fi

rclone copy "$FILE" "$REMOTE"

find "$BACKUP_DIR" -name 'hira2-db-*.sql.gz' -mtime +7 -delete
rclone delete "$REMOTE" --min-age 30d --include 'hira2-db-*'

echo "$(date -Is) OK: $FILE ($SIZE bytes)"
