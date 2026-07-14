#!/bin/bash
# Fast dev deploy: build the arm64 image (with registry layer cache), push to
# Docker Hub, and update a Pi over SSH. Code-only changes deploy in ~1-2 min.
#
# Usage:
#   scripts/deploy-pi.sh <user@pi-host> [tag]
#
# Examples:
#   scripts/deploy-pi.sh babypi@192.168.1.92          # deploy :latest
#   scripts/deploy-pi.sh babypi@192.168.1.92 dev      # deploy :dev tag
#
# Requirements: docker login (Hub), buildx multiarch builder, ssh key on the Pi.

set -euo pipefail

PI_HOST="${1:?usage: deploy-pi.sh <user@pi-host> [tag]}"
TAG="${2:-latest}"
IMAGE_REPO="${IMAGE_REPO:-gunsmoke/babymonitor}"
IMAGE="$IMAGE_REPO:$TAG"
CACHE_REF="$IMAGE_REPO:buildcache"

log() { echo -e "\033[1;32m[deploy]\033[0m $*"; }

# Dev deploys only READ the registry cache; writing it (~2 min) is done by
# release builds (see AGENTS.md). Set CACHE_PUSH=1 to refresh it from here,
# e.g. after changing dependencies (go.mod, pip packages, apt).
CACHE_TO=()
[ "${CACHE_PUSH:-0}" = "1" ] && CACHE_TO=(--cache-to "type=registry,ref=$CACHE_REF,mode=max")

log "Building $IMAGE (linux/arm64) with registry cache..."
docker buildx build \
    --platform linux/arm64 \
    --cache-from "type=registry,ref=$CACHE_REF" \
    "${CACHE_TO[@]}" \
    -t "$IMAGE" \
    --push .

log "Updating $PI_HOST..."
ssh "$PI_HOST" "cd ~/babymonitor && \
    sudo env BABYMONITOR_IMAGE=$IMAGE docker compose pull -q && \
    sudo env BABYMONITOR_IMAGE=$IMAGE docker compose up -d --no-build"

log "Waiting for health..."
for i in $(seq 1 20); do
    STATUS="$(ssh "$PI_HOST" "sudo docker inspect --format '{{.State.Health.Status}}' babymonitor 2>/dev/null" || echo starting)"
    if [ "$STATUS" = "healthy" ]; then
        log "Deployed. $PI_HOST is healthy — http://${PI_HOST#*@}:8080"
        exit 0
    fi
    sleep 3
done

echo "[deploy] Container not healthy after 60s — check: ssh $PI_HOST 'sudo docker logs babymonitor'" >&2
exit 1
