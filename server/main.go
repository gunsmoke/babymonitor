package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// --- Config ---

type Schedule struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`    // "HH:MM" format
	Stop     string `json:"stop"`     // "HH:MM" format
	Days     []int  `json:"days"`     // 0=Sun, 1=Mon, ..., 6=Sat
}

type Config struct {
	Interval      float64    `json:"interval"`
	Amplification float64    `json:"amplification"`
	MinContrast   float64    `json:"min_contrast"`
	Consecutive   int        `json:"consecutive"`
	Fraction      float64    `json:"fraction"`
	ProbThreshold float64    `json:"prob_threshold"`
	Cooldown      int        `json:"cooldown"`
	ThresholdOnly bool       `json:"threshold_only"`
	MicDevice     string     `json:"mic_device"`
	Schedules     []Schedule `json:"schedules"`
}

func defaultConfig() Config {
	return Config{
		Interval:      5.0,
		Amplification: 10.0,
		MinContrast:   15.0,
		Consecutive:   6,
		Fraction:      0.5,
		ProbThreshold: 0.80,
		Cooldown:      180,
		ThresholdOnly: false,
		MicDevice:     "",
		Schedules:     []Schedule{},
	}
}

var (
	configPath string
	homeDir    string
	baseDir    string
)

func loadConfig() (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func saveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// --- Audio Device Discovery ---

type AudioDevice struct {
	ID        string `json:"id"`
	Label     string `json:"label"` // human-friendly display name
	Card      string `json:"card"`
	CardNum   string `json:"card_num"`
	Device    string `json:"device"`
	DeviceNum string `json:"device_num"`
	Type      string `json:"type"` // "capture" or "playback"
}

func listAudioDevices(deviceType string) ([]AudioDevice, error) {
	cmd := "arecord"
	dtype := "capture"
	if deviceType == "playback" {
		cmd = "aplay"
		dtype = "playback"
	}

	out, err := exec.Command(cmd, "-l").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", cmd, err, string(out))
	}

	re := regexp.MustCompile(`card (\d+): (\w+) \[(.+?)\], device (\d+): (.+?) \[(.+?)\]`)
	matches := re.FindAllStringSubmatch(string(out), -1)

	var devices []AudioDevice
	for _, m := range matches {
		devices = append(devices, AudioDevice{
			ID:        fmt.Sprintf("hw:%s,%s", m[1], m[4]),
			Label:     m[3],
			CardNum:   m[1],
			Card:      m[3],
			DeviceNum: m[4],
			Device:    m[6],
			Type:      dtype,
		})
	}
	return devices, nil
}

// listPulseAudioSources returns PulseAudio/PipeWire input sources (includes
// Bluetooth). It parses the full `pactl list sources` output to get the
// human-friendly Description and the backing ALSA card number, which is used
// to dedupe against the raw ALSA device list.
func listPulseAudioSources() []AudioDevice {
	out, err := exec.Command("pactl", "list", "sources").CombinedOutput()
	if err != nil {
		return nil
	}

	var devices []AudioDevice
	var name, desc, alsaCard string
	flush := func() {
		if name == "" || strings.Contains(name, ".monitor") {
			return
		}
		label := desc
		if label == "" {
			label = name
		}
		if strings.Contains(name, "bluez") || strings.Contains(name, "bluetooth") {
			label += " (Bluetooth)"
		}
		devices = append(devices, AudioDevice{
			ID:      "pulse:" + name,
			Label:   label,
			Card:    label,
			CardNum: alsaCard,
			Device:  name,
			Type:    "capture",
		})
	}

	alsaCardRe := regexp.MustCompile(`alsa\.card\s*=\s*"(\d+)"`)
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Source #"):
			flush()
			name, desc, alsaCard = "", "", ""
		case strings.HasPrefix(trimmed, "Name:"):
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "Name:"))
		case strings.HasPrefix(trimmed, "Description:"):
			desc = strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))
		default:
			if m := alsaCardRe.FindStringSubmatch(trimmed); m != nil {
				alsaCard = m[1]
			}
		}
	}
	flush()
	return devices
}

// --- Detector Process Management ---

type DetectorState struct {
	mu              sync.RWMutex
	running         bool
	stoppedManually bool
	cmd             *exec.Cmd
	lastCfg         Config
	logs            []LogEntry
	maxLogs         int
	clients         map[chan LogEntry]bool
	clientMu        sync.Mutex
}

type LogEntry struct {
	Time    string `json:"time"`
	Message string `json:"message"`
	Level   string `json:"level"` // "info", "alert", "quiet", "detection"
}

var detector = &DetectorState{
	maxLogs: 500,
	clients: make(map[chan LogEntry]bool),
}

func (d *DetectorState) addLog(entry LogEntry) {
	d.mu.Lock()
	d.logs = append(d.logs, entry)
	if len(d.logs) > d.maxLogs {
		d.logs = d.logs[len(d.logs)-d.maxLogs:]
	}
	d.mu.Unlock()

	// Broadcast to SSE clients
	d.clientMu.Lock()
	for ch := range d.clients {
		select {
		case ch <- entry:
		default:
			// Drop if client is slow
		}
	}
	d.clientMu.Unlock()
}

func (d *DetectorState) subscribe() chan LogEntry {
	ch := make(chan LogEntry, 50)
	d.clientMu.Lock()
	d.clients[ch] = true
	d.clientMu.Unlock()
	return ch
}

func (d *DetectorState) unsubscribe(ch chan LogEntry) {
	d.clientMu.Lock()
	delete(d.clients, ch)
	d.clientMu.Unlock()
	close(ch)
}

func (d *DetectorState) Start(cfg Config) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("detector already running")
	}
	d.running = true
	d.stoppedManually = false
	d.lastCfg = cfg
	d.logs = nil
	d.mu.Unlock()

	setDesiredRunning(true)

	d.addLog(LogEntry{
		Time:    time.Now().Format("15:04:05"),
		Message: "Detector starting...",
		Level:   "info",
	})
	logEvent("start", "Detector started", 0, "")

	go d.runDetector(cfg)
	return nil
}

func (d *DetectorState) runDetector(cfg Config) {
	const maxRetries = 10
	backoff := 2 * time.Second

	for attempt := 0; ; attempt++ {
		exitCode := d.runDetectorOnce(cfg)

		d.mu.RLock()
		stopped := d.stoppedManually
		d.mu.RUnlock()

		if stopped {
			// Manual stop — don't restart
			break
		}

		// Process exited unexpectedly
		if attempt >= maxRetries {
			d.addLog(LogEntry{
				Time:    time.Now().Format("15:04:05"),
				Message: fmt.Sprintf("Detector crashed %d times, giving up. Will not auto-restart.", maxRetries),
				Level:   "alert",
			})
			logEvent("alert", fmt.Sprintf("Detector crashed %d times, stopped retrying", maxRetries), 0, "")
			setDesiredRunning(false)
			break
		}

		wait := backoff * time.Duration(1<<uint(min(attempt, 5))) // max ~64s
		d.addLog(LogEntry{
			Time:    time.Now().Format("15:04:05"),
			Message: fmt.Sprintf("Detector exited (code %d), restarting in %s (attempt %d/%d)...", exitCode, wait, attempt+1, maxRetries),
			Level:   "alert",
		})
		logEvent("alert", fmt.Sprintf("Detector crashed (exit %d), restarting attempt %d/%d", exitCode, attempt+1, maxRetries), 0, "")

		time.Sleep(wait)

		d.mu.RLock()
		stopped = d.stoppedManually
		d.mu.RUnlock()
		if stopped {
			break
		}
	}

	d.mu.Lock()
	d.running = false
	d.mu.Unlock()
	d.addLog(LogEntry{
		Time:    time.Now().Format("15:04:05"),
		Message: "Detector stopped",
		Level:   "info",
	})
	logEvent("stop", "Detector stopped", 0, "")
}

func (d *DetectorState) runDetectorOnce(cfg Config) int {
	pythonBin := os.Getenv("BABY_MONITOR_PYTHON")
	if pythonBin == "" {
		pythonBin = filepath.Join(homeDir, "babymonitor_env", "bin", "python")
	}
	scriptPath := os.Getenv("BABY_MONITOR_DETECTOR")
	if scriptPath == "" {
		scriptPath = filepath.Join(homeDir, "babymonitor", "baby_monitor.py")
	}

	args := []string{
		"-u", scriptPath,
		"--interval", fmt.Sprintf("%.1f", cfg.Interval),
		"--amplification", fmt.Sprintf("%.1f", cfg.Amplification),
		"--min-contrast", fmt.Sprintf("%.1f", cfg.MinContrast),
		"--consecutive", fmt.Sprintf("%d", cfg.Consecutive),
		"--fraction", fmt.Sprintf("%.2f", cfg.Fraction),
		"--prob-threshold", fmt.Sprintf("%.2f", cfg.ProbThreshold),
		"--cooldown", fmt.Sprintf("%d", cfg.Cooldown),
	}
	if cfg.ThresholdOnly {
		args = append(args, "--threshold-only")
	}
	if cfg.MicDevice != "" {
		args = append(args, "--mic-device", cfg.MicDevice)
	}

	cmd := exec.Command(pythonBin, args...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		d.addLog(LogEntry{Time: time.Now().Format("15:04:05"), Message: "Pipe error: " + err.Error(), Level: "alert"})
		return -1
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		d.addLog(LogEntry{Time: time.Now().Format("15:04:05"), Message: "Start error: " + err.Error(), Level: "alert"})
		return -1
	}

	d.mu.Lock()
	d.cmd = cmd
	d.mu.Unlock()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		level := classifyLogLine(line)
		d.addLog(LogEntry{
			Time:    time.Now().Format("15:04:05"),
			Message: line,
			Level:   level,
		})
		logDetectionLine(line)
	}

	err = cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return -1
	}
	return 0
}

func (d *DetectorState) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running || d.cmd == nil {
		return fmt.Errorf("detector not running")
	}

	d.stoppedManually = true
	setDesiredRunning(false)

	if err := d.cmd.Process.Signal(os.Interrupt); err != nil {
		d.cmd.Process.Kill()
	}
	d.running = false
	return nil
}

func (d *DetectorState) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

func (d *DetectorState) GetLogs() []LogEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	copied := make([]LogEntry, len(d.logs))
	copy(copied, d.logs)
	return copied
}

func classifyLogLine(line string) string {
	switch {
	case strings.Contains(line, "*** ALERT"):
		return "alert"
	case strings.Contains(line, "quiet") || strings.Contains(line, "skipping inference"):
		return "quiet"
	case strings.Contains(line, "CRYING=") || strings.Contains(line, "ambient="):
		return "detection"
	default:
		return "info"
	}
}

// logDetectionLine parses detector output and logs relevant events to SQLite
var detectionRe = regexp.MustCompile(`ambient=(\d+)%\s+CRYING=(\d+)%\s+babbling=(\d+)%\s+->\s+(\w+)`)

func logDetectionLine(line string) {
	if strings.Contains(line, "*** ALERT") {
		logEvent("alert", line, 0, "")
		return
	}
	if m := detectionRe.FindStringSubmatch(line); m != nil {
		classification := m[4]
		var prob float64
		fmt.Sscanf(m[2], "%f", &prob)
		prob = prob / 100.0
		eventType := classification
		if classification == "CRYING" {
			eventType = "crying"
		}
		logEvent(eventType, line, prob, classification)
	}
}

// --- HTTP Handlers ---

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, indexHTML)
}

func handleGetDevices(w http.ResponseWriter, r *http.Request) {
	alsa, _ := listAudioDevices("capture")
	playback, _ := listAudioDevices("playback")
	pulse := listPulseAudioSources()

	// Merge, deduplicating: PulseAudio sources wrap ALSA cards (alsa.card
	// property), so drop raw hw: entries already represented by a source.
	// Pulse entries come first — they have friendlier names and also work
	// for Bluetooth devices.
	capture := pulse
	pulseCards := map[string]bool{}
	for _, p := range pulse {
		if p.CardNum != "" {
			pulseCards[p.CardNum] = true
		}
	}
	for _, a := range alsa {
		if !pulseCards[a.CardNum] {
			capture = append(capture, a)
		}
	}

	resp := map[string]interface{}{
		"capture":  capture,
		"playback": playback,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadConfig()
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var cfg Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if err := saveConfig(cfg); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	// Sync schedules to crontab
	if err := syncCrontab(cfg.Schedules); err != nil {
		log.Printf("Warning: failed to sync crontab: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleDetectorStart(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadConfig()
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if err := detector.Start(cfg); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func handleDetectorStop(w http.ResponseWriter, r *http.Request) {
	if err := detector.Stop(); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func handleDetectorStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"running": detector.IsRunning(),
		"logs":    detector.GetLogs(),
	})
}

func handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, "streaming not supported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := detector.subscribe()
	defer detector.unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-ch:
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- Crontab Scheduler ---

const crontabMarkerStart = "# BABYMONITOR-SCHEDULE-START"
const crontabMarkerEnd = "# BABYMONITOR-SCHEDULE-END"

// Read the current user's crontab, stripping our managed block
func readCrontabWithout() (string, error) {
	out, err := exec.Command("crontab", "-l").CombinedOutput()
	if err != nil {
		// No crontab yet is fine
		if strings.Contains(string(out), "no crontab") {
			return "", nil
		}
		return "", fmt.Errorf("crontab -l: %s", string(out))
	}

	lines := strings.Split(string(out), "\n")
	var kept []string
	inBlock := false
	for _, l := range lines {
		if strings.TrimSpace(l) == crontabMarkerStart {
			inBlock = true
			continue
		}
		if strings.TrimSpace(l) == crontabMarkerEnd {
			inBlock = false
			continue
		}
		if !inBlock {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n"), nil
}

// cronDays converts our day ints (0=Sun..6=Sat) to cron format
func cronDays(days []int) string {
	if len(days) == 0 || len(days) == 7 {
		return "*"
	}
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = fmt.Sprintf("%d", d)
	}
	return strings.Join(parts, ",")
}

// syncCrontab writes our schedule entries into the user's crontab
func syncCrontab(schedules []Schedule) error {
	existing, err := readCrontabWithout()
	if err != nil {
		return err
	}

	var block []string
	block = append(block, crontabMarkerStart)

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	for i, s := range schedules {
		if !s.Enabled {
			continue
		}
		startH, startM, err := parseHHMM(s.Start)
		if err != nil {
			continue
		}
		stopH, stopM, err := parseHHMM(s.Stop)
		if err != nil {
			continue
		}
		days := cronDays(s.Days)

		block = append(block, fmt.Sprintf("# Schedule %d: %s-%s", i+1, s.Start, s.Stop))
		block = append(block, fmt.Sprintf("%d %d * * %s curl -s -X POST http://localhost:%s/api/detector/start > /dev/null 2>&1",
			startM, startH, days, port))
		block = append(block, fmt.Sprintf("%d %d * * %s curl -s -X POST http://localhost:%s/api/detector/stop > /dev/null 2>&1",
			stopM, stopH, days, port))
	}

	block = append(block, crontabMarkerEnd)

	// Combine existing + our block
	full := strings.TrimRight(existing, "\n")
	if full != "" {
		full += "\n"
	}
	full += strings.Join(block, "\n") + "\n"

	// Write via pipe to crontab
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(full)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("crontab install failed: %s: %w", string(out), err)
	}

	log.Printf("[scheduler] Synced %d schedule(s) to crontab", len(schedules))
	return nil
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format: %s", s)
	}
	var h, m int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return 0, 0, err
	}
	return h, m, nil
}

func handleScheduleStatus(w http.ResponseWriter, r *http.Request) {
	cfg, _ := loadConfig()

	// Read actual crontab to show what's installed
	out, _ := exec.Command("crontab", "-l").CombinedOutput()
	crontabLines := ""
	inBlock := false
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) == crontabMarkerStart {
			inBlock = true
			continue
		}
		if strings.TrimSpace(l) == crontabMarkerEnd {
			inBlock = false
			continue
		}
		if inBlock && strings.TrimSpace(l) != "" {
			if crontabLines != "" {
				crontabLines += "\n"
			}
			crontabLines += l
		}
	}

	now := time.Now()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"schedules":    cfg.Schedules,
		"crontab":      crontabLines,
		"current_time": now.Format("15:04"),
		"current_day":  int(now.Weekday()),
	})
}

// powerPipePath is a FIFO created by the Docker entrypoint. A root-owned
// helper loop reads actions from it and forwards them to the HOST's
// systemd-logind over the bind-mounted D-Bus socket, so reboot/shutdown
// affect the host machine, not just the container.
const powerPipePath = "/run/babymonitor/power"

func powerControlAvailable() bool {
	if fi, err := os.Stat(powerPipePath); err == nil && fi.Mode()&os.ModeNamedPipe != 0 {
		return true
	}
	_, err := exec.LookPath("sudo")
	return err == nil
}

func performPowerAction(action string) {
	// Docker: hand off to the root helper in the entrypoint
	if fi, err := os.Stat(powerPipePath); err == nil && fi.Mode()&os.ModeNamedPipe != 0 {
		f, err := os.OpenFile(powerPipePath, os.O_WRONLY, 0)
		if err == nil {
			defer f.Close()
			if _, err := f.WriteString(action + "\n"); err == nil {
				return
			}
		}
		log.Printf("[power] pipe write failed: %v — trying fallback", err)
	}
	// Bare metal: classic sudo path
	if action == "reboot" {
		exec.Command("sudo", "-n", "reboot").Run()
	} else {
		exec.Command("sudo", "-n", "shutdown", "-h", "now").Run()
	}
}

func handlePowerAction(action, status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !powerControlAvailable() {
			jsonError(w, "Power control not available in this environment", 501)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": status})
		go func() {
			time.Sleep(500 * time.Millisecond)
			performPowerAction(action)
		}()
	}
}

// inDocker reports whether we are running inside a container.
func inDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]string{}

	// Hostname — inside Docker os.Hostname() is the container ID, so prefer
	// the host's hostname bind-mounted by docker-compose.
	if data, err := os.ReadFile("/etc/host-hostname"); err == nil && strings.TrimSpace(string(data)) != "" {
		info["hostname"] = strings.TrimSpace(string(data))
	} else if h, err := os.Hostname(); err == nil {
		info["hostname"] = h
	}

	// Uptime — /proc/uptime is the host kernel's uptime, works in containers too.
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if secs, err := strconv.ParseFloat(fields[0], 64); err == nil {
				info["uptime"] = formatUptime(time.Duration(secs) * time.Second)
			}
		}
	}

	// CPU temp — sysfs is the host kernel's.
	if data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		temp := strings.TrimSpace(string(data))
		if len(temp) > 3 {
			info["cpu_temp"] = temp[:len(temp)-3] + "." + temp[len(temp)-3:len(temp)-2] + "°C"
		}
	}

	// Memory — /proc/meminfo is host-wide.
	if total, avail, ok := readMemInfo(); ok {
		info["memory"] = fmt.Sprintf("%s / %s used", humanBytes(total-avail), humanBytes(total))
	}

	// Disk — stat the data dir (host-backed volume) instead of parsing df of the overlay.
	var st syscall.Statfs_t
	if err := syscall.Statfs(baseDir, &st); err == nil && st.Blocks > 0 {
		total := uint64(st.Bsize) * st.Blocks
		free := uint64(st.Bsize) * st.Bavail
		info["disk"] = fmt.Sprintf("%s / %s used", humanBytes(total-free), humanBytes(total))
	}

	// Address — inside Docker the container IP is useless; report the address
	// the client actually reached us on.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	info["ip"] = host

	if inDocker() {
		info["environment"] = "Docker"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func formatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func readMemInfo() (total, avail uint64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = kb * 1024
		case "MemAvailable:":
			avail = kb * 1024
		}
	}
	return total, avail, total > 0 && avail > 0
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(b)/float64(div), "KMGTP"[exp])
}

// --- History Handlers ---

func handleHistoryStats(w http.ResponseWriter, r *http.Request) {
	stats, err := getTodayStats()
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleHistoryEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	events, err := getRecentEvents(limit)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if events == nil {
		events = []RecentEvent{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func handleHistoryDaily(w http.ResponseWriter, r *http.Request) {
	days := 7
	if d := r.URL.Query().Get("days"); d != "" {
		fmt.Sscanf(d, "%d", &days)
	}
	summary, err := getDailySummary(days)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if summary == nil {
		summary = []EventSummary{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func handleHistoryChart(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view") // "today", "week", "month"
	if view == "" {
		view = "today"
	}

	var points []ChartPoint
	var err error

	switch view {
	case "today":
		date := r.URL.Query().Get("date")
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		points, err = getHourlyChart(date)
	case "week":
		points, err = getDailyChart(7)
	case "month":
		points, err = getDailyChart(30)
	default:
		jsonError(w, "invalid view: use today, week, or month", 400)
		return
	}

	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if points == nil {
		points = []ChartPoint{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(points)
}

func handleHistoryAlerts(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	alerts, err := getRecentAlerts(limit)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if alerts == nil {
		alerts = []RecentEvent{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alerts)
}

// --- Main ---

func main() {
	var err error
	homeDir, err = os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	// Support env overrides for Docker
	baseDir = os.Getenv("BABY_MONITOR_DIR")
	if baseDir == "" {
		baseDir = filepath.Join(homeDir, "babymonitor")
	}
	configPath = filepath.Join(baseDir, "config.json")

	// Ensure config dir exists
	os.MkdirAll(filepath.Dir(configPath), 0755)

	// Save default config if none exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		saveConfig(defaultConfig())
	}

	// Init SQLite
	if err := initDB(); err != nil {
		log.Printf("Warning: failed to init database: %v", err)
	}

	// Sync schedules to crontab on startup
	if cfg, err := loadConfig(); err == nil && len(cfg.Schedules) > 0 {
		if err := syncCrontab(cfg.Schedules); err != nil {
			log.Printf("Warning: failed to sync crontab: %v", err)
		}
	}

	// Restore detector state from DB
	if getDesiredRunning() {
		log.Printf("Restoring detector state: was running before shutdown")
		if cfg, err := loadConfig(); err == nil {
			go func() {
				if err := detector.Start(cfg); err != nil {
					log.Printf("Failed to auto-start detector: %v", err)
				}
			}()
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("GET /api/devices", handleGetDevices)
	mux.HandleFunc("GET /api/config", handleGetConfig)
	mux.HandleFunc("POST /api/config", handleSaveConfig)
	mux.HandleFunc("POST /api/config/reset", func(w http.ResponseWriter, r *http.Request) {
		cfg := defaultConfig()
		saveConfig(cfg)
		json.NewEncoder(w).Encode(cfg)
	})
	mux.HandleFunc("POST /api/detector/start", handleDetectorStart)
	mux.HandleFunc("POST /api/detector/stop", handleDetectorStop)
	mux.HandleFunc("GET /api/detector/status", handleDetectorStatus)
	mux.HandleFunc("GET /api/detector/stream", handleSSE)
	mux.HandleFunc("GET /api/system", handleSystemInfo)
	mux.HandleFunc("GET /api/schedule/status", handleScheduleStatus)
	mux.HandleFunc("POST /api/system/reboot", handlePowerAction("reboot", "rebooting"))
	mux.HandleFunc("POST /api/system/shutdown", handlePowerAction("shutdown", "shutting down"))
	mux.HandleFunc("GET /api/history/stats", handleHistoryStats)
	mux.HandleFunc("GET /api/history/events", handleHistoryEvents)
	mux.HandleFunc("GET /api/history/daily", handleHistoryDaily)
	mux.HandleFunc("GET /api/history/chart", handleHistoryChart)
	mux.HandleFunc("GET /api/history/alerts", handleHistoryAlerts)

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	log.Printf("Baby Monitor Web UI starting on http://0.0.0.0:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
