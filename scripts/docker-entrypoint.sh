#!/bin/bash
set -e

# --- Audio device access ---
# Match babypi user to host audio GID for /dev/snd access
if [ -d /dev/snd ]; then
    HOST_AUDIO_GID=$(stat -c '%g' /dev/snd/controlC0 2>/dev/null || echo "")
    if [ -n "$HOST_AUDIO_GID" ]; then
        CONTAINER_AUDIO_GID=$(getent group audio | cut -d: -f3)
        if [ "$HOST_AUDIO_GID" != "$CONTAINER_AUDIO_GID" ]; then
            groupadd -g "$HOST_AUDIO_GID" hostaudio 2>/dev/null || true
            usermod -aG hostaudio babypi 2>/dev/null || true
        fi
    fi
    echo "[entrypoint] ALSA devices found:"
    arecord -l 2>/dev/null | grep -E "^card" || echo "  (none)"
else
    echo "[entrypoint] WARNING: /dev/snd not found — no direct ALSA mic access"
fi

# --- PulseAudio / PipeWire (for Bluetooth devices) ---
# Detect host PulseAudio socket
PULSE_FOUND=false
for PULSE_PATH in \
    "/run/user/1000/pulse/native" \
    "/run/user/$(id -u babypi)/pulse/native" \
    "/tmp/pulse-socket" \
    "/run/pulse/native"; do
    if [ -S "$PULSE_PATH" ]; then
        export PULSE_SERVER="unix:$PULSE_PATH"
        PULSE_FOUND=true
        echo "[entrypoint] PulseAudio socket: $PULSE_PATH"
        break
    fi
done

if [ "$PULSE_FOUND" = false ]; then
    echo "[entrypoint] No PulseAudio socket found — Bluetooth devices won't be available"
    echo "[entrypoint] Using direct ALSA only (USB microphones)"
    # Remove PulseAudio defaults from ALSA config so arecord falls back to hw:
    if [ -f /etc/asound.conf ]; then
        cat > /etc/asound.conf <<'ALSA'
# PulseAudio not available — use ALSA directly
# BT devices require PulseAudio socket passthrough
ALSA
    fi
fi

# Pass PULSE_SERVER to babypi user environment
if [ -n "$PULSE_SERVER" ]; then
    echo "export PULSE_SERVER=$PULSE_SERVER" >> /home/babypi/.bashrc
    # Also write PulseAudio client config
    mkdir -p /home/babypi/.config/pulse
    cat > /home/babypi/.config/pulse/client.conf <<PULSECONF
default-server = $PULSE_SERVER
autospawn = no
PULSECONF
    chown -R babypi:babypi /home/babypi/.config
fi

# List all available audio sources (ALSA + PulseAudio/BT)
if [ "$PULSE_FOUND" = true ]; then
    echo "[entrypoint] PulseAudio sources (includes Bluetooth):"
    su -s /bin/bash babypi -c "PULSE_SERVER=$PULSE_SERVER pactl list sources short 2>/dev/null" || echo "  (could not query)"
fi

# --- Start cron daemon (needed for schedule feature) ---
service cron start 2>/dev/null || cron 2>/dev/null || true

# --- Ensure data dir permissions ---
chown -R babypi:babypi /app/data 2>/dev/null || true

echo "[entrypoint] Starting Baby Monitor..."

# --- Run the Go server as babypi user ---
# setpriv (not su) so the Go server is PID 1's exec target and receives
# SIGTERM from `docker stop` directly for graceful shutdown.
exec setpriv --reuid=babypi --regid=babypi --init-groups \
    env HOME=/home/babypi PULSE_SERVER="${PULSE_SERVER:-}" /app/babymonitor-server
