# --- Stage 1: Build Go server ---
FROM golang:1.25-bookworm AS builder

WORKDIR /build
COPY server/ .
RUN go mod download && CGO_ENABLED=0 go build -o babymonitor-server .

# --- Stage 2: Runtime ---
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-venv python3-pip \
    alsa-utils \
    libasound2-plugins \
    pulseaudio-utils \
    libpulse0 \
    cron \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# ALSA PulseAudio plugin config — makes BT/PipeWire devices visible to arecord
RUN echo 'pcm.pulse {\n  type pulse\n}\nctl.pulse {\n  type pulse\n}\n# Make PulseAudio the default if available\npcm.!default {\n  type pulse\n}\nctl.!default {\n  type pulse\n}' > /etc/asound.conf

# Create non-root user with audio group
RUN useradd -m -s /bin/bash babypi && \
    usermod -aG audio,pulse-access babypi 2>/dev/null || \
    usermod -aG audio babypi

WORKDIR /app

# Copy Go binary
COPY --from=builder /build/babymonitor-server .

# Copy Python detector + bundled libs
COPY detector/ ./detector/

# Create Python venv and install deps
RUN python3 -m venv /app/venv && \
    /app/venv/bin/pip install --no-cache-dir \
    numpy scipy python_speech_features opencv-python-headless

# Create persistent directories
RUN mkdir -p /app/data /app/model && \
    chown -R babypi:babypi /app

# Copy entrypoint
COPY scripts/docker-entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

ENV BABY_MONITOR_DIR=/app/data
ENV BABY_MONITOR_DETECTOR=/app/detector/baby_monitor.py
ENV BABY_MONITOR_PYTHON=/app/venv/bin/python
ENV BABY_MONITOR_MODEL_DIR=/app/model
ENV PYTHONUNBUFFERED=1

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -fsS http://localhost:8080/api/detector/status || exit 1

VOLUME ["/app/data", "/app/model"]

ENTRYPOINT ["/app/entrypoint.sh"]
