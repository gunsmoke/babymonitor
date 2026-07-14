#!/bin/bash
# Baby Monitor - one-line installer for Raspberry Pi / any Debian-based Linux
#
#   curl -fsSL https://raw.githubusercontent.com/gunsmoke/babymonitor/main/scripts/install.sh | bash
#
# What it does:
#   1. Installs Docker (via get.docker.com) if missing
#   2. Clones (or updates) this repo to ~/babymonitor
#   3. Removes any old bare-metal install (service, crontab entries, files)
#   4. Pulls the prebuilt image from Docker Hub (or builds locally as fallback)
#      and starts the container fresh
#
# Options (env vars):
#   REPO_URL=...          override the git repo URL
#   INSTALL_DIR=...       override install location (default: ~/babymonitor)
#   BABYMONITOR_IMAGE=... override the Docker Hub image (default: gunsmoke/babymonitor:latest)
#   BUILD=1               force building the image locally instead of pulling
#   PURGE=1               also delete old bare-metal leftovers (venv, model clone)

set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/gunsmoke/babymonitor.git}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/babymonitor}"
export BABYMONITOR_IMAGE="${BABYMONITOR_IMAGE:-gunsmoke/babymonitor:latest}"

log()  { echo -e "\033[1;32m[install]\033[0m $*"; }
warn() { echo -e "\033[1;33m[install]\033[0m $*"; }
die()  { echo -e "\033[1;31m[install]\033[0m $*" >&2; exit 1; }

# --- Sanity checks -----------------------------------------------------------
[ "$(id -u)" -eq 0 ] && die "Run as a normal user (with sudo access), not root."
command -v sudo >/dev/null || die "sudo is required."
# 'sudo true' (not 'sudo -v') — -v can demand a password even with NOPASSWD rules
sudo true || die "This script needs sudo access."

case "$(uname -m)" in
    aarch64|x86_64|armv7l) ;;
    *) warn "Untested architecture: $(uname -m) — continuing anyway" ;;
esac

# If the script lives inside a checkout of the repo, install from there.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-/nonexistent}")" 2>/dev/null && pwd || true)"
if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/../docker-compose.yml" ] && [ -f "$SCRIPT_DIR/../model/crynet_large.onnx" ]; then
    INSTALL_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
    LOCAL_COPY=1
else
    LOCAL_COPY=0
fi

# --- 1. Docker ---------------------------------------------------------------
if command -v docker >/dev/null 2>&1; then
    log "Docker already installed: $(docker --version)"
else
    log "Installing Docker (this takes a few minutes on a Pi)..."
    curl -fsSL https://get.docker.com | sudo sh
    sudo usermod -aG docker "$USER"
    log "Docker installed. (You'll be able to use docker without sudo after re-login.)"
fi
sudo systemctl enable --now docker >/dev/null 2>&1 || true

# --- 2. Remove old bare-metal install (fresh setup, no data migration) -------
# The pre-Docker version ran a systemd service from ~/babymonitor with a
# venv in ~/babymonitor_env and schedule entries in the user's crontab.
if systemctl list-unit-files 2>/dev/null | grep -q '^babymonitor\.service'; then
    log "Old bare-metal babymonitor.service found — stopping and removing it"
    sudo systemctl stop babymonitor 2>/dev/null || true
    sudo systemctl disable babymonitor 2>/dev/null || true
    sudo rm -f /etc/systemd/system/babymonitor.service
    sudo systemctl daemon-reload
fi

# Remove schedule entries the old install wrote to the HOST crontab
if crontab -l 2>/dev/null | grep -q 'BABYMONITOR-SCHEDULE-START'; then
    log "Removing old babymonitor entries from host crontab"
    crontab -l 2>/dev/null | sed '/# BABYMONITOR-SCHEDULE-START/,/# BABYMONITOR-SCHEDULE-END/d' | crontab -
fi

OLD_DIR="$HOME/babymonitor"
if [ -f "$OLD_DIR/babymonitor-server" ] && [ ! -f "$OLD_DIR/docker-compose.yml" ]; then
    log "Removing old bare-metal install dir $OLD_DIR"
    rm -rf "$OLD_DIR"
fi

# --- 3. Get the code ---------------------------------------------------------
if [ "$LOCAL_COPY" = "1" ]; then
    log "Installing from local copy: $INSTALL_DIR"
elif [ -d "$INSTALL_DIR/.git" ]; then
    log "Updating existing checkout in $INSTALL_DIR"
    git -C "$INSTALL_DIR" pull --ff-only
else
    command -v git >/dev/null || { log "Installing git..."; sudo apt-get update -qq && sudo apt-get install -y -qq git; }
    log "Cloning $REPO_URL into $INSTALL_DIR"
    git clone --depth 1 "$REPO_URL" "$INSTALL_DIR"
fi
cd "$INSTALL_DIR"

[ -f model/crynet_large.onnx ] || die "model/crynet_large.onnx missing from $INSTALL_DIR."

# --- 4. PulseAudio socket path (for Bluetooth mics) --------------------------
PULSE_DIR="/run/user/$(id -u)/pulse"
[ -S "$PULSE_DIR/native" ] || warn "No PulseAudio socket at $PULSE_DIR — Bluetooth mics won't work (USB mics are fine)."
export PULSE_DIR

# --- 5. Pull (or build) and start --------------------------------------------
if [ "${BUILD:-0}" = "1" ]; then
    log "BUILD=1 — building the image locally (10-20 min on a Pi)..."
    sudo -E docker compose up -d --build
elif sudo -E docker compose pull 2>/dev/null; then
    log "Pulled prebuilt image $BABYMONITOR_IMAGE — starting..."
    sudo -E docker compose up -d --no-build
else
    warn "Could not pull $BABYMONITOR_IMAGE — building locally (10-20 min on a Pi)..."
    sudo -E docker compose up -d --build
fi

# --- 6. Wait for health ------------------------------------------------------
log "Waiting for the server to come up..."
for i in $(seq 1 30); do
    if curl -fsS -o /dev/null http://localhost:8080/ 2>/dev/null; then
        IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
        log ""
        log "=========================================="
        log "  Baby Monitor is running!"
        log "  Web UI:  http://${IP:-localhost}:8080"
        log "=========================================="
        if [ "${PURGE:-0}" = "1" ]; then
            log "PURGE=1 — removing old bare-metal leftovers"
            rm -rf "$HOME/babymonitor_env" "$HOME/OpenBabyMonitor"
        fi
        exit 0
    fi
    sleep 2
done

die "Server did not respond on :8080 after 60s. Check: sudo docker logs babymonitor"
