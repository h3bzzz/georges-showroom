#!/usr/bin/env bash
# Move the VPS from the v1 stack to v2: dump the workstation's georges2 through a reverse
# tunnel (feed_products left out: it is the raw store, not read by the site), restore it into
# the VPS Postgres as georges2, then start the v2 compose (docker-compose.v2.yml staged in
# /srv/georges) with IMAGE_TAG. Run from the repo root after deploy/sync-data.sh images.
#
#   deploy/switch-v2.sh <image sha>
set -euo pipefail
cd "$(dirname "$0")/.."
VPS=${VPS:-root@160.238.36.50}
TAG=${1:?image tag (commit sha) required}
LOCAL_URL=${DATABASE_URL:-postgres://georges:georges@127.0.0.1:5432/georges2?sslmode=disable}
LOCAL_URL=${LOCAL_URL/localhost/127.0.0.1}
TUNNEL_URL=${LOCAL_URL/127.0.0.1:5432/127.0.0.1:5439}

echo "== dump georges2 → VPS (reverse tunnel 5439)"
ssh -o ExitOnForwardFailure=yes -R 127.0.0.1:5439:127.0.0.1:5432 "$VPS" "
  set -e
  docker run --rm --network host -v /srv/georges:/dump postgres:16 \
    pg_dump -Fc --no-owner --exclude-table-data=feed_products '$TUNNEL_URL' -f /dump/georges2.dump
  ls -la /srv/georges/georges2.dump"

echo "== restore into the db container as georges2"
ssh "$VPS" "
  set -e
  DB=\$(docker ps -qf name=georges-showroom-db)
  docker exec \$DB psql -U georges -d postgres -tAc \"SELECT 1 FROM pg_database WHERE datname='georges2'\" | grep -q 1 || \
    docker exec \$DB psql -U georges -d postgres -c 'CREATE DATABASE georges2'
  docker cp /srv/georges/georges2.dump \$DB:/tmp/georges2.dump
  docker exec \$DB pg_restore -U georges -d georges2 --clean --if-exists --no-owner -j 2 /tmp/georges2.dump || true
  docker exec \$DB rm -f /tmp/georges2.dump
  docker exec \$DB psql -U georges -d georges2 -tAc 'SELECT (SELECT count(*) FROM families), (SELECT count(*) FROM items), (SELECT count(*) FROM search_docs)'"

echo "== switch compose to v2 with IMAGE_TAG=$TAG"
ssh "$VPS" "
  set -e
  cd /srv/georges
  cp docker-compose.yml docker-compose.v1.bak
  cp docker-compose.v2.yml docker-compose.yml
  sed -i 's/^IMAGE_TAG=.*/IMAGE_TAG=$TAG/' .env
  install -d -o 10001 -g 10001 data/cache data/covers
  docker compose -p georges-showroom up -d --pull always --remove-orphans
  sleep 8
  docker ps --format '{{.Names}} {{.Status}}'
  curl -s -o /dev/null -w 'app %{http_code}\n' http://127.0.0.1:80/healthz"
echo "done"
