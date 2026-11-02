#!/bin/bash
# Backup of a wishlist install, for cron (e.g. 15 4 * * * /bin/bash backup.sh).
#
# One archive holds everything a restore needs:
#   data/     the data volume: wishlist.json (gifts, reservations with guest
#             emails, settings with shipping addresses), session.key and
#             images/
#   project/  docker-compose.yml and .env from the compose directory
#
# Taken while the wishlist stays up: wishlist.json is only ever replaced by
# an atomic rename, so tar reads either the old or the new file, never a
# half-written one. A picture is written once, when its gift is added.
#
# .env and wishlist.json hold secrets and personal data, so the backup
# directory is kept at 700 and the archives at 600.
#
# Restore, with the wishlist stopped (docker compose stop wishlist):
#   docker run --rm --user 0 -v wishlist_data:/data -v "$BACKUP_DIR:/b:ro" \
#     --entrypoint sh wishlist:local -c 'cd /data && tar -xzf /b/<archive> --strip-components=1 data/'
set -euo pipefail

WISHLIST_DIR="${WISHLIST_DIR:-$HOME/wishlist}"            # docker-compose.yml and .env
BACKUP_DIR="${BACKUP_DIR:-$HOME/wishlist-backups}"
VOLUME="${VOLUME:-wishlist_data}"
IMAGE="${IMAGE:-wishlist:local}"
KEEP="${KEEP:-14}"                                         # newest archives kept
LOG="${LOG:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/wishlist-backup.log}"
DOCKER="${DOCKER:-/usr/bin/docker}"

exec >> "$LOG" 2>&1
echo "=== $(date "+%Y-%m-%d %H:%M:%S") start ==="

umask 077
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

NAME="wishlist_backup_$(date +%Y-%m-%d_%H-%M-%S).tar.gz"
TMP="$BACKUP_DIR/.$NAME.part"
trap 'rm -f "$TMP"' EXIT

# The image runs as uid 1000, the owner of the volume's files. Run from
# root's crontab, .env is root's too, so tar reads as root.
AS_ROOT=()
[ "$(id -u)" -eq 0 ] && AS_ROOT=(--user 0)
$DOCKER run --rm --network none "${AS_ROOT[@]}" --entrypoint tar \
  -v "$VOLUME:/backup/data:ro" \
  -v "$WISHLIST_DIR/docker-compose.yml:/backup/project/docker-compose.yml:ro" \
  -v "$WISHLIST_DIR/.env:/backup/project/.env:ro" \
  "$IMAGE" -czf - -C /backup data project > "$TMP"

# Refuse an archive that is broken or lacks the data file. grep reads to the
# end: with -q it would exit early, tar would die of SIGPIPE, and pipefail
# would call a good archive bad.
tar -tzf "$TMP" | grep -x "data/wishlist.json" >/dev/null || { echo "!! data/wishlist.json missing from the archive"; exit 1; }

mv "$TMP" "$BACKUP_DIR/$NAME"
echo "wrote $NAME ($(du -h "$BACKUP_DIR/$NAME" | cut -f1))"

cd "$BACKUP_DIR"
ls -1t wishlist_backup_*.tar.gz | tail -n +$((KEEP + 1)) | xargs -r rm --

echo "=== $(date "+%Y-%m-%d %H:%M:%S") end ==="
