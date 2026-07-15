# Baby Monitor

AI-powered baby cry detection system using neural network inference. Detects crying, babbling, and ambient sounds in real-time with a mobile-first web UI. Sends vibration alerts to a Bangle.js smartwatch over BLE. Runs on Raspberry Pi or any Linux host via Docker.

## Features

- Real-time audio classification (crying / babbling / ambient) via ONNX neural network
- **Sliding window detection** — catches intermittent crying (cry-pause-cry) that simple streak counters miss
- Mobile-first baby-themed web UI with live status, Chart.js history graphs, and alert log
- **BLE smartwatch support** — vibration alerts + full app on Bangle.js (start/stop/dismiss from wrist)
- Schedule-based auto start/stop via crontab (runs inside the container)
- Crash recovery with exponential backoff (auto-restarts detector up to 10 times)
- Survives reboots (persists desired state in SQLite)
- USB microphone support via ALSA + Bluetooth mic support via PulseAudio
- **Self-update from web UI** — pull latest Docker image with progress overlay
- **System cleanup** — clear history, compact DB, prune old Docker images
- Fully self-contained Docker deployment — nothing needed on the host except Docker

## Architecture

```
┌─────────────────────────────────────────────────┐
│  Docker Container                               │
│                                                 │
│  ┌───────────────────────────────────────────┐  │
│  │  Go Web Server (port 8080)                │  │
│  │  - REST API + SSE live stream             │  │
│  │  - SQLite (events, state, BLE devices)    │  │
│  │  - Manages Python subprocess              │  │
│  │  - Syncs schedules to crontab             │  │
│  │  - BLE connection manager (Bangle.js)     │  │
│  └──────────────┬────────────────────────────┘  │
│                 │ spawns                         │
│  ┌──────────────▼────────────────────────────┐  │
│  │  Python Detector (baby_monitor.py)        │  │
│  │  - ALSA capture (USB) or PulseAudio (BT)  │  │
│  │  - MFCC feature extraction                │  │
│  │  - ONNX inference (crynet_large model)    │  │
│  │  - Sliding window cry detection           │  │
│  │  - Outputs classification to stdout       │  │
│  └───────────────────────────────────────────┘  │
│                                                 │
│  cron daemon (schedule start/stop)              │
│  PulseAudio client (Bluetooth audio)            │
└─────────────────────────────────────────────────┘
     │           │           │           │
  /dev/snd   PulseAudio   port 8080   BLE (BlueZ)
  (USB mic)   socket       (Web UI)   (Smartwatch)
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

## BLE Smartwatch (Bangle.js)

The baby monitor can send vibration alerts to a Bangle.js smartwatch over Bluetooth Low Energy (BLE). When crying is detected, the watch vibrates in an SOS-like pattern and shows a prompt to dismiss or stop monitoring.

### Setup

1. Make sure Bluetooth is enabled on the Pi: `sudo rfkill unblock bluetooth`
2. Open the web UI and scroll to the **Smartwatch** card
3. Click **Scan for devices** — your Bangle.js will appear
4. Click **+ Add** next to it — the watch connects and the app installs automatically
5. The Bangle.js app ("Baby Monitor") appears in the watch launcher with a baby icon

### Watch App Features

- **Main screen**: Shows monitoring status (Listening/Stopped), alert and cry counts
- **Listening screen**: Shows the listening icon centered, with compact stats
- **Alert screen**: "Baby Crying" title with crying baby icon, SOS vibration pattern that repeats until dismissed
- **BTN2**: Start/Stop monitoring, or Dismiss alert when one is active
- **BTN3**: Stop monitor during alert, or return to launcher

### How It Works

- Uses Nordic UART Service (NUS) over BLE via `tinygo.org/x/bluetooth` (pure Go, no CGO)
- The Go server connects to the Bangle.js, writes the app to its Storage, and launches it
- On cry alert: server sends JS command via NUS that triggers vibration + alert screen
- Watch sends JSON commands back (`{"t":"cmd","cmd":"dismiss|start|stop"}`)
- Auto-reconnects every 30 seconds if the watch disconnects
- Paired devices are stored in SQLite and survive container restarts

### Docker BLE Requirements

BLE uses the BlueZ D-Bus API via the `/run/dbus` mount (already in `docker-compose.yml`). If BLE doesn't work, try adding `privileged: true` to the compose file.

## Bluetooth Microphone Support

If you want to use a Bluetooth microphone (smartwatch, BT headset, etc.), the host must have the device paired and connected via PulseAudio or PipeWire. The container accesses BT audio through the host's PulseAudio socket:

```yaml
# docker-compose.yml already includes:
volumes:
  - /run/user/1000/pulse:/run/user/1000/pulse  # PulseAudio socket
  - /run/dbus:/run/dbus:ro                     # D-Bus (needed for BT)
```

> **Note:** Replace `1000` with your host user's UID if different (`id -u`).

The BT devices appear in the mic selector dropdown alongside USB devices.

## Detection Algorithm

The detector uses a **sliding window** approach instead of a simple consecutive streak:

- Tracks the last N inference results (default window size: 10)
- Quiet/skipped checks (below noise floor) are not counted — only actual NN predictions enter the window
- Alerts when the ratio of crying detections in the window exceeds the threshold (default: 50%)
- This catches intermittent crying patterns (cry-pause-cry-pause) that a strict consecutive-streak counter would miss
- A cooldown period (default: 180s) prevents alert spam

**Example:** With window=10 and ratio=50%, a pattern like `cry-quiet-cry-cry-quiet-cry-cry-cry-quiet-cry` (7/10 = 70%) triggers an alert, while a single false positive (1/10 = 10%) does not.

## Configuration

All settings are tunable from the web UI. Click "Reset Defaults" to restore sane values.

| Setting | Default | Description |
|---------|---------|-------------|
| Interval | 5.0s | Seconds between audio samples |
| Amplification | 10.0 | Audio amplification factor |
| Min Contrast | 15.0 dB | Signal-over-background threshold for inference |
| Detection Window | 10 | Number of recent detections to track in sliding window |
| Cry Ratio | 0.50 | Alert when this fraction of the window is crying |
| Prob Threshold | 0.80 | Neural network confidence threshold |
| Cooldown | 180s | Minimum seconds between consecutive alerts |
| BLE Alerts | On | Send alerts to connected smartwatches |

## Self-Update

The web UI includes a self-update feature in the **Device Info** card:

1. Click the **Update** button
2. A fullscreen modal shows real-time progress:
   - Checking current version
   - Downloading latest image from Docker Hub
   - Comparing versions (skips if already latest)
   - Cleaning up old images
   - Restarting the container
3. The UI automatically reconnects after restart

This requires the Docker socket to be mounted (included in `docker-compose.yml`).

## System Cleanup

The **Device Info** card also provides maintenance tools:

- **Cleanup** — Removes events older than 7 days, compacts the SQLite database (VACUUM), prunes unused Docker images and build cache. Shows progress in a modal.
- **Clear History** — Deletes ALL detection events and vacuums the database. Frees disk space immediately.
- **Storage stats** — Shows event count and database size at a glance.

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
│   ├── main.go                # HTTP handlers, detector mgmt, SSE, crontab, update
│   ├── db.go                  # SQLite: events, stats, charts, BLE devices, state
│   ├── ble.go                 # BLE connection manager (Bangle.js via Nordic UART)
│   ├── ui.go                  # Embedded single-page HTML/CSS/JS UI
│   ├── go.mod / go.sum
├── detector/                  # Python cry detection
│   ├── baby_monitor.py        # Main detector (ALSA + PulseAudio, sliding window)
│   └── lib/                   # Bundled feature extraction (from OpenBabyMonitor)
│       ├── features.py
│       └── librosa_destilled.py
├── scripts/
│   ├── docker-entrypoint.sh   # Container init (cron, audio GID, PulseAudio, BLE)
│   ├── deploy-pi.sh           # Fast dev deploy to Pi via Docker Hub
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
| `GET` | `/api/system` | System info (CPU, memory, uptime, disk, DB stats) |
| `POST` | `/api/system/reboot` | Reboot the host |
| `POST` | `/api/system/shutdown` | Shut down the host |
| `POST` | `/api/system/update` | Self-update (pull latest image, prune, restart) |
| `GET` | `/api/system/cleanup` | System cleanup (SSE progress stream) |
| `POST` | `/api/system/clear-history` | Delete all detection events |
| `GET` | `/api/ble/scan` | Scan for BLE devices (10s, filtered to Bangle.js) |
| `GET` | `/api/ble/status` | Get BLE connection status for all devices |
| `POST` | `/api/ble/add` | Add a BLE device `{address, name}` |
| `POST` | `/api/ble/remove` | Remove a BLE device `{address}` |
| `POST` | `/api/ble/connect` | Reconnect to a device `{address}` |
| `POST` | `/api/ble/disconnect` | Disconnect a device `{address}` |
| `POST` | `/api/ble/test` | Send test alert to all connected devices |

## Hardware

- **Minimum:** Raspberry Pi 3B+ (1GB RAM, ARM64) or any Linux x86_64 machine
- **Microphone:** USB mic (tested with Logitech C920) or Bluetooth mic/smartwatch
- **Smartwatch (optional):** Bangle.js 1 or 2 for wrist alerts
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

5. **Fast deploy to Pi** (~30-60s for code changes):
   ```bash
   scripts/deploy-pi.sh babypi@192.168.1.92
   ```

### Code Style

- **Go:** Standard `gofmt`. No external linter config needed.
- **Python:** PEP 8. No strict formatter enforced.
- **UI/CSS:** Use CSS variables from `:root`. Mobile-first (`min-width` breakpoints). No external CSS frameworks.
- **Commits:** Short imperative subject line. Describe _why_, not _what_.

### Project Conventions

- All timestamps are stored in **local time** (not UTC)
- Config lives in `config.json` next to the binary
- BLE devices are stored in SQLite `ble_devices` table (survives restarts)
- Persistent state uses the SQLite `state` table (key/value pairs)
- The Go server manages the Python detector as a subprocess — do not try to run them independently in production
- Detection output is parsed via regex: `ambient=(\d+)%\s+CRYING=(\d+)%\s+babbling=(\d+)%\s+->\s+(\w+)`
- BLE uses `tinygo.org/x/bluetooth` (pure Go, BlueZ D-Bus on Linux, no CGO)

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
- Support for more BLE smartwatches (Pinetime, Watchy, etc.)

## Credits

- Neural network model: [OpenBabyMonitor](https://github.com/Bieltveansen/OpenBabyMonitor)
- Pure-Go SQLite: [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
- Pure-Go BLE: [tinygo.org/x/bluetooth](https://pkg.go.dev/tinygo.org/x/bluetooth)
- Charts: [Chart.js](https://www.chartjs.org/)

## License

MIT
