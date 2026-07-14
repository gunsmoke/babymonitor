# Baby Monitor

AI-powered baby cry detection system using neural network inference. Detects crying, babbling, and ambient sounds in real-time with a mobile-first web UI. Runs on Raspberry Pi or any Linux host via Docker.

## Features

- Real-time audio classification (crying / babbling / ambient) via ONNX neural network
- Mobile-first baby-themed web UI with live status, Chart.js history graphs, and alert log
- Schedule-based auto start/stop via crontab (runs inside the container)
- Crash recovery with exponential backoff (auto-restarts detector up to 10 times)
- Survives reboots (persists desired state in SQLite)
- USB microphone support via ALSA + Bluetooth mic support via PulseAudio
- Fully self-contained Docker deployment — nothing needed on the host except Docker

## Architecture

```
┌─────────────────────────────────────────────────┐
│  Docker Container                               │
│                                                 │
│  ┌───────────────────────────────────────────┐  │
│  │  Go Web Server (port 8080)                │  │
│  │  - REST API + SSE live stream             │  │
│  │  - SQLite (events, state, config)         │  │
│  │  - Manages Python subprocess              │  │
│  │  - Syncs schedules to crontab             │  │
│  └──────────────┬────────────────────────────┘  │
│                 │ spawns                         │
│  ┌──────────────▼────────────────────────────┐  │
│  │  Python Detector (baby_monitor.py)        │  │
│  │  - ALSA capture (USB) or PulseAudio (BT)  │  │
│  │  - MFCC feature extraction                │  │
│  │  - ONNX inference (crynet_large model)    │  │
│  │  - Outputs classification to stdout       │  │
│  └───────────────────────────────────────────┘  │
│                                                 │
│  cron daemon (schedule start/stop)              │
│  PulseAudio client (Bluetooth audio)            │
└─────────────────────────────────────────────────┘
        │              │              │
   /dev/snd      PulseAudio       port 8080
   (USB mic)      socket           (Web UI)
                 (BT audio)
```

## Quick Start — One-Line Install (Raspberry Pi / any Debian-based Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/gunsmoke/babymonitor/main/scripts/install.sh | bash
```

The installer:
1. Installs Docker if missing
2. Clones this repo to `~/babymonitor` (model files included — no extra downloads)
3. Removes any old bare-metal install (systemd service, crontab entries) for a fresh start
4. Pulls the prebuilt image from Docker Hub ([`gunsmoke/babymonitor`](https://hub.docker.com/r/gunsmoke/babymonitor), arm64 + amd64) and starts it, then prints the Web UI URL

Takes ~2 minutes on a Pi. When it finishes, open `http://<pi-ip>:8080`.

Options: `BUILD=1` builds the image locally instead of pulling; `PURGE=1` also deletes old bare-metal leftovers; `INSTALL_DIR=...` changes install location.

### Manual install

```bash
git clone https://github.com/gunsmoke/babymonitor.git
cd babymonitor
docker compose up -d --build
```

Then open `http://<your-ip>:8080`.

### Bluetooth / Smart Watch Support

If you want to use a Bluetooth microphone (smartwatch, BT headset, etc.), the host must have the device paired and connected via PulseAudio or PipeWire. The container accesses BT audio through the host's PulseAudio socket:

```yaml
# docker-compose.yml already includes:
volumes:
  - /run/user/1000/pulse:/run/user/1000/pulse  # PulseAudio socket
  - /run/dbus:/run/dbus:ro                     # D-Bus (needed for BT)
```

> **Note:** Replace `1000` with your host user's UID if different (`id -u`).

The BT devices appear in the mic selector dropdown alongside USB devices.

## Configuration

All settings are tunable from the web UI. Click "Reset Defaults" to restore sane values.

| Setting | Default | Description |
|---------|---------|-------------|
| Interval | 5.0s | Seconds between audio samples |
| Amplification | 10.0 | Audio amplification factor |
| Min Contrast | 15.0 dB | Signal-over-background threshold for inference |
| Consecutive | 6 | Consecutive cry detections before triggering alert |
| Fraction | 0.50 | Foreground audio fraction for loudness analysis |
| Prob Threshold | 0.80 | Neural network confidence threshold |
| Cooldown | 180s | Minimum seconds between consecutive alerts |

## Environment Variables

Used for Docker path configuration. Not needed for bare-metal (uses `~/babymonitor/` defaults).

| Variable | Default | Description |
|----------|---------|-------------|
| `BABY_MONITOR_DIR` | `~/babymonitor` | Config + SQLite database directory |
| `BABY_MONITOR_DETECTOR` | `~/babymonitor/baby_monitor.py` | Python detector script path |
| `BABY_MONITOR_PYTHON` | `~/babymonitor_env/bin/python` | Python interpreter path |
| `BABY_MONITOR_MODEL_DIR` | `~/OpenBabyMonitor` | ONNX model + feature libs directory |
| `TZ` | System | Timezone (e.g., `Europe/London`) |

## Project Structure

```
babymonitor/
├── server/                    # Go web server
│   ├── main.go                # HTTP handlers, detector mgmt, SSE, crontab sync
│   ├── db.go                  # SQLite: events, stats, charts, persistent state
│   ├── ui.go                  # Embedded single-page HTML/CSS/JS UI
│   ├── go.mod / go.sum
├── detector/                  # Python cry detection
│   ├── baby_monitor.py        # Main detector (ALSA + PulseAudio support)
│   └── lib/                   # Bundled feature extraction (from OpenBabyMonitor)
│       ├── features.py
│       └── librosa_destilled.py
├── scripts/
│   ├── docker-entrypoint.sh   # Container init (cron, audio GID, PulseAudio)
│   └── install.sh             # One-line installer (Docker + deploy + migration)
├── model/                     # ONNX model + feature libs (bind-mounted read-only)
├── .opencode/                 # OpenCode agent configs
│   ├── opencode.json
│   └── agents/
│       ├── deploy.md          # Build & deploy agent
│       ├── debug.md           # Diagnostics agent
│       └── ui.md              # UI modification agent
├── Dockerfile                 # Multi-stage build (Go + Python runtime)
├── docker-compose.yml
├── AGENTS.md                  # Agent system prompt / project context
└── README.md
```

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/config` | Get current configuration |
| `POST` | `/api/config` | Save configuration |
| `POST` | `/api/config/reset` | Reset config to defaults |
| `GET` | `/api/devices` | List audio devices (ALSA + PulseAudio) |
| `POST` | `/api/detector/start` | Start the detector |
| `POST` | `/api/detector/stop` | Stop the detector |
| `GET` | `/api/detector/status` | Get detector status + recent logs |
| `GET` | `/api/detector/stream` | SSE event stream (live updates) |
| `GET` | `/api/history/chart?view=today\|week\|month` | Chart data |
| `GET` | `/api/history/stats` | Today's statistics |
| `GET` | `/api/history/alerts?limit=N` | Recent cry alerts |
| `GET` | `/api/history/events?limit=N` | Recent detection events |
| `GET` | `/api/schedule/status` | Schedule configuration + crontab |
| `GET` | `/api/system/info` | System info (CPU, memory, uptime) |
| `POST` | `/api/system/reboot` | Reboot the host |
| `POST` | `/api/system/shutdown` | Shut down the host |

## Hardware

- **Minimum:** Raspberry Pi 3B+ (1GB RAM, ARM64) or any Linux x86_64 machine
- **Microphone:** USB mic (tested with Logitech C920) or Bluetooth mic/smartwatch
- **No GPU required** — ONNX inference runs on CPU

---

## Contributing

### Prerequisites

- Go 1.22+
- Python 3.11+
- Docker & Docker Compose (for container testing)

### Development Setup

```bash
# Clone the repo
git clone <this-repo>
cd babymonitor

# Install Go dependencies
cd server && go mod download && cd ..

# Create a Python venv for local testing
python3 -m venv venv
source venv/bin/activate
pip install numpy scipy python_speech_features opencv-python-headless
```

### Development Workflow

1. **Go server changes** (`server/`):
   ```bash
   # Run locally (connects to local mic)
   cd server && go run .

   # Or cross-compile for Pi
   GOOS=linux GOARCH=arm64 go build -o babymonitor-server .
   ```

2. **UI changes** (`server/ui.go`):
   - The UI is a single Go string constant — edit it directly
   - Rebuild and refresh the browser to see changes
   - Use the CSS variables (see `.opencode/agents/ui.md` for the palette)

3. **Python detector changes** (`detector/`):
   ```bash
   # Test locally
   python detector/baby_monitor.py --threshold-only --interval 3
   ```

4. **Docker testing**:
   ```bash
   docker compose up -d --build
   docker compose logs -f
   ```

### Code Style

- **Go:** Standard `gofmt`. No external linter config needed.
- **Python:** PEP 8. No strict formatter enforced.
- **UI/CSS:** Use CSS variables from `:root`. Mobile-first (`min-width` breakpoints). No external CSS frameworks.
- **Commits:** Short imperative subject line. Describe _why_, not _what_.

### Project Conventions

- All timestamps are stored in **local time** (not UTC)
- Config lives in `config.json` next to the binary
- Persistent state uses the SQLite `state` table (key/value pairs)
- The Go server manages the Python detector as a subprocess — do not try to run them independently in production
- Detection output is parsed via regex: `ambient=(\d+)%\s+CRYING=(\d+)%\s+babbling=(\d+)%\s+->\s+(\w+)`

### Submitting Changes

1. Fork the repo
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes
4. Test locally or via Docker
5. Commit with a clear message
6. Open a Pull Request with:
   - What you changed and why
   - How to test it
   - Screenshots if it's a UI change

### Areas for Contribution

- Camera/video monitoring support
- Push notifications (Pushover, Telegram, webhook)
- Multi-room support (multiple mics)
- Dark mode toggle
- Audio playback of detected events
- Better mobile PWA support
- Unit tests for Go handlers and Python detector
- Accessibility improvements (ARIA labels, keyboard nav)

## Credits

- Neural network model: [OpenBabyMonitor](https://github.com/Bieltveansen/OpenBabyMonitor)
- Pure-Go SQLite: [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
- Charts: [Chart.js](https://www.chartjs.org/)

## License

MIT
