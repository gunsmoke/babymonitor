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
- BLE via `tinygo.org/x/bluetooth` (pure Go, BlueZ D-Bus on Linux, no CGO)
- UI is embedded as a Go string constant in `server/ui.go` (single-page, no build step)
- Python detector outputs to stdout, Go parses lines via regex
- Audio: ALSA (`arecord`) for USB mics, PulseAudio (`parec`) for Bluetooth devices
- Detection regex: `ambient=(\d+)%\s+CRYING=(\d+)%\s+babbling=(\d+)%\s+->\s+(\w+)`
- Env vars for Docker paths: `BABY_MONITOR_DIR`, `BABY_MONITOR_PYTHON`, `BABY_MONITOR_DETECTOR`, `BABY_MONITOR_MODEL_DIR`

## BLE Smartwatch Support (Bangle.js)
- `server/ble.go` — BLE connection manager using Nordic UART Service (NUS)
- Connects to Bangle.js (1 or 2) and other Espruino devices over BLE
- On cry alert: sends vibration + notification prompt to smartwatch
- Smartwatch can dismiss alerts, start/stop monitoring
- Auto-reconnects every 30s to disconnected devices
- NUS UUIDs: Service `6e400001-...`, RX (write) `6e400002-...`, TX (notify) `6e400003-...`
- BLE API endpoints: `GET /api/ble/scan`, `GET /api/ble/status`, `POST /api/ble/add`, `POST /api/ble/remove`, `POST /api/ble/connect`, `POST /api/ble/disconnect`, `POST /api/ble/test`
- Config fields: `ble_devices` (array of `{address, name, type}`), `ble_alerts` (bool)
- JS code is sent to Bangle.js at connect time via NUS to set up alert handler
- Commands from Bangle.js arrive as JSON: `{"t":"cmd","cmd":"dismiss|start|stop"}`
- Docker: requires `--privileged` or D-Bus + BLE device access for BLE to work

## Build & Deploy
- Local build/run: `docker compose up -d --build`
- Fast dev deploy to a Pi (~30-60s for code changes): `scripts/deploy-pi.sh <user@pi-host> [tag]`
- Release: `docker buildx build --platform linux/amd64,linux/arm64 --cache-from type=registry,ref=gunsmoke/babymonitor:buildcache --cache-to type=registry,ref=gunsmoke/babymonitor:buildcache,mode=max -t gunsmoke/babymonitor:latest --push .`
- Pi/user install: `bash scripts/install.sh` (pulls prebuilt image, falls back to local build)
- Dockerfile is 3-stage: builder (Go), runtime-base (apt + pip, slow/stable), final (app code, fast). Keep code COPYs in the final stage so dev rebuilds stay fast.
- buildx builder needs `--driver-opt network=host` on hosts with IPv6-only DNS answers

## Conventions
- All timestamps stored in local time (not UTC)
- Config persisted in `config.json` in `BABY_MONITOR_DIR` (Docker volume `babymonitor-data`)
- State persisted in SQLite `state` table (key/value)
- Mobile-first CSS with `min-width` media queries
- Baby-themed pastel color palette (see CSS variables in `ui.go`)
