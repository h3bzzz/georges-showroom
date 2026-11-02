#!/usr/bin/env bash
# Open an SSH tunnel to the VPS database so the workstation pipeline writes to the live store
# directly (no dump/restore). Prints the DATABASE_URL to export, then holds the tunnel open.
#
#   deploy/tunnel.sh            local port 5433 → VPS postgres (loopback)
#   DATABASE_URL=$(deploy/tunnel.sh --url) georges ingest best
#
# The password is the VPS's /srv/georges/.env POSTGRES_PASSWORD; the script reads it over ssh
# once and never prints it to the terminal unless --url is asked for.
set -euo pipefail
VPS=${VPS:-root@160.238.36.50}
PORT=${TUNNEL_PORT:-5433}
pw() { ssh "$VPS" "grep ^POSTGRES_PASSWORD= /srv/georges/.env | cut -d= -f2-"; }
url() { echo "postgres://georges:$(pw)@127.0.0.1:$PORT/georges?sslmode=disable"; }
if [ "${1:-}" = "--url" ]; then url; exit 0; fi
if ss -ltn 2>/dev/null | grep -q ":$PORT "; then
  echo "tunnel already open on 127.0.0.1:$PORT" >&2
else
  ssh -f -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -L "127.0.0.1:$PORT:127.0.0.1:5432" "$VPS"
  echo "tunnel open: 127.0.0.1:$PORT → $VPS postgres" >&2
fi
echo "export DATABASE_URL='$(url)'"
