# Baby Monitor - Agent Instructions

## Project Overview
AI-powered baby cry detection system. Go web server manages a Python subprocess that records audio, runs ONNX neural network inference, and classifies sounds as crying/babbling/ambient.

## Architecture
- `server/` — Go web server (HTTP API, SSE, SQLite, crontab scheduling, process management)
- `detector/` — Python detection script + bundled OpenBabyMonitor feature extraction
- `scripts/` — Docker entrypoint and one-line installer (`install.sh`)
- `Dockerfile` + `docker-compose.yml` — Containerized deployment (image: `gunsmoke/babymonitor` on Docker Hub)

## Key Technical Details
- Go 1.22+ ServeMux: route patterns must include method prefix (`"POST /api/..."`)
- Pure-Go SQLite via `modernc.org/sqlite` (no CGO)
- UI is embedded as a Go string constant in `server/ui.go` (single-page, no build step)
- Python detector outputs to stdout, Go parses lines via regex
- Audio: ALSA (`arecord`) for USB mics, PulseAudio (`parec`) for Bluetooth devices
- Detection regex: `ambient=(\d+)%\s+CRYING=(\d+)%\s+babbling=(\d+)%\s+->\s+(\w+)`
- Env vars for Docker paths: `BABY_MONITOR_DIR`, `BABY_MONITOR_PYTHON`, `BABY_MONITOR_DETECTOR`, `BABY_MONITOR_MODEL_DIR`

## Build & Deploy
- Local build/run: `docker compose up -d --build`
- Release: `docker buildx build --platform linux/amd64,linux/arm64 -t gunsmoke/babymonitor:latest --push .`
- Pi/user install: `bash scripts/install.sh` (pulls prebuilt image, falls back to local build)
- Cross-compile (dev only): `GOOS=linux GOARCH=arm64 go build -o babymonitor-server ./server/`

## Conventions
- All timestamps stored in local time (not UTC)
- Config persisted in `config.json` in `BABY_MONITOR_DIR` (Docker volume `babymonitor-data`)
- State persisted in SQLite `state` table (key/value)
- Mobile-first CSS with `min-width` media queries
- Baby-themed pastel color palette (see CSS variables in `ui.go`)
