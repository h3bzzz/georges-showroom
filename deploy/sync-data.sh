#!/usr/bin/env bash
# Publish the workstation's data to the VPS: the image roots the storefront serves and a fresh
# copy of the database. Code ships by itself on every push to main (.github/workflows/deploy.yml);
# this is the other half, run after the pipeline on the workstation changes crops, links or rows.
#
#   deploy/sync-data.sh            images + database
#   deploy/sync-data.sh images     images only
#   deploy/sync-data.sh db         database only
#
# Needs: ssh key on the VPS (root), Docker stack running there (compose project georges-showroom),
# the local embedded Postgres answering on DATABASE_URL (georges serve or georges db start).
set -euo pipefail
cd "$(dirname "$0")/.."
VPS=${VPS:-root@160.238.36.50}
REMOTE_DATA=${REMOTE_DATA:-/srv/georges/data}
LOCAL_DATA=${DATA_DIR:-data/raw}
DB_URL=${DATABASE_URL:-postgres://georges:georges@127.0.0.1:5432/georges?sslmode=disable}
ROOTS=(assets crops crops_hd logos webimg catalogs specimg docs tools)
what=${1:-all}

images() {
  local src=()
  for r in "${ROOTS[@]}"; do [ -d "$LOCAL_DATA/$r" ] && src+=("$LOCAL_DATA/$r"); done
  echo "== images → $VPS:$REMOTE_DATA"
  # --chmod: the app runs as uid 10001 inside the container and only needs to read the masters
  rsync -a --info=progress2 --no-inc-recursive --chmod=Do+rx,Fo+r "${src[@]}" "$VPS:$REMOTE_DATA/"
  ssh "$VPS" "install -d -o 10001 -g 10001 $REMOTE_DATA/cache $REMOTE_DATA/covers"
}

db() {
  echo "== database → $VPS (pg_dump through a reverse tunnel, restore into the db container)"
  # the VPS reaches the workstation's Postgres on its own loopback:5439 for the length of this
  # ssh (5432 there is the live database, published on loopback for deploy/tunnel.sh)
  local url=${DB_URL/localhost/127.0.0.1}
  url=${url/127.0.0.1:5432/127.0.0.1:5439}
  ssh -o ExitOnForwardFailure=yes -R 127.0.0.1:5439:127.0.0.1:5432 "$VPS" "
    set -e
    docker run --rm --network host -v /srv/georges:/dump postgres:16 pg_dump -Fc '$url' -f /dump/georges.dump
    DB=\$(docker ps -qf name=georges-showroom-db)
    docker cp /srv/georges/georges.dump \$DB:/tmp/georges.dump
    docker exec \$DB pg_restore -U georges -d georges --no-owner --clean --if-exists /tmp/georges.dump 2>&1 | grep -vE 'does not exist, skipping' || true
    docker exec \$DB psql -U georges -d georges -tAc 'ANALYZE'
    docker restart georges-showroom-app-1 >/dev/null
    docker exec \$DB psql -U georges -d georges -tAc \"select 'families='||count(*) from product_families\"
  "
}

case "$what" in
  all) images; db ;;
  images) images ;;
  db) db ;;
  *) echo "usage: $0 [all|images|db]" >&2; exit 2 ;;
esac
echo "== done"
