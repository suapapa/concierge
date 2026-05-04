#!/bin/sh
set -e
# Named/bind mounts are often root:root; the app runs as appuser and needs
# create/write on the luggage temp dir (active_refs.lock, uploads, etc.).
TMP="${CONCIERGE_TMP_DIR:-/app/concierge_archive}"
mkdir -p "$TMP"
chown -R appuser:appgroup "$TMP"
exec su-exec appuser "$@"
