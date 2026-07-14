# syntax=docker/dockerfile:1

# --- Stage 1: Build Go server ---
FROM golang:1.25-bookworm AS builder

WORKDIR /build
# Dependency layer first — only invalidated when go.mod/go.sum change
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
# Source layer — code changes only rebuild from here
COPY server/ .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o babymonitor-server .

# --- Stage 2: Runtime base (OS packages + Python deps; heavy, rarely changes) ---
FROM debian:bookworm-slim AS runtime-base

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

# Python venv with ML deps — the slowest layer, kept independent of app code
RUN --mount=type=cache,target=/root/.cache/pip \
    python3 -m venv /app/venv && \
    /app/venv/bin/pip install \
    numpy scipy python_speech_features opencv-python-headless

# --- Stage 3: Final image (fast-changing app code layers last) ---
FROM runtime-base

WORKDIR /app

# Persistent directories
RUN mkdir -p /app/data /app/model && chown -R babypi:babypi /app

# Entrypoint
COPY scripts/docker-entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# App code — changing these only rebuilds the layers below
COPY detector/ ./detector/
COPY --from=builder /build/babymonitor-server .

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
