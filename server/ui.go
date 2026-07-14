package main

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Baby Monitor</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=Nunito:wght@400;600;700;800&display=swap');
  :root {
    --bg: #faf5f0;
    --surface: #ffffff;
    --border: #f0e6dc;
    --text: #4a3f35;
    --muted: #a8977f;
    --accent: #7eb8d0;
    --accent-soft: #d4ecf5;
    --pink: #e8a0b4;
    --pink-soft: #fce4ec;
    --green: #7bc67e;
    --green-soft: #e8f5e9;
    --red: #e07070;
    --red-soft: #fde8e8;
    --yellow: #f0c56d;
    --yellow-soft: #fef9e7;
    --lavender: #b8a9d4;
    --lavender-soft: #f0ecf7;
    --shadow: 0 2px 12px rgba(74,63,53,0.06);
    --shadow-lg: 0 4px 24px rgba(74,63,53,0.10);
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: 'Nunito', -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    background: var(--bg); color: var(--text);
    min-height: 100vh; min-height: 100dvh;
    padding: 0.5rem;
    -webkit-text-size-adjust: 100%;
    background-image: radial-gradient(circle at 20% 50%, rgba(126,184,208,0.08) 0%, transparent 50%),
                      radial-gradient(circle at 80% 20%, rgba(232,160,180,0.08) 0%, transparent 50%),
                      radial-gradient(circle at 60% 80%, rgba(184,169,212,0.08) 0%, transparent 50%);
  }
  .container { max-width: 820px; margin: 0 auto; }
  @media (min-width: 640px) { body { padding: 1rem; } }

  /* Header */
  header { text-align: center; padding: 1rem 0 0.5rem; margin-bottom: 1rem; }
  .header-icon { font-size: 2rem; margin-bottom: 0.15rem; }
  header h1 { font-size: 1.3rem; font-weight: 800; letter-spacing: -0.02em; }
  header .subtitle { font-size: 0.8rem; color: var(--muted); margin-top: 0.15rem; }
  @media (min-width: 640px) {
    header { padding: 1.5rem 0 1rem; margin-bottom: 1.5rem; }
    .header-icon { font-size: 2.5rem; }
    header h1 { font-size: 1.6rem; }
    header .subtitle { font-size: 0.85rem; }
  }

  /* Status bar */
  .status-bar {
    display: flex; align-items: center; justify-content: center; gap: 0.6rem;
    padding: 0.6rem 1rem; border-radius: 999px; margin: 0.75rem auto;
    font-weight: 700; font-size: 0.85rem; transition: all 0.3s ease;
  }
  @media (min-width: 640px) {
    .status-bar { padding: 0.75rem 1.5rem; max-width: 400px; font-size: 0.95rem; gap: 1rem; margin: 1rem auto; }
  }
  .status-bar.listening { background: var(--green-soft); color: #3a7d3d; border: 2px solid var(--green); }
  .status-bar.sleeping { background: var(--lavender-soft); color: #6b5b8d; border: 2px solid var(--lavender); }
  .status-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; animation: pulse 2s infinite; }
  .listening .status-dot { background: var(--green); }
  .sleeping .status-dot { background: var(--lavender); animation: none; }
  @keyframes pulse {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.5; transform: scale(0.8); }
  }

  /* Big buttons */
  .big-buttons {
    display: flex; gap: 0.5rem; justify-content: center; margin: 0.75rem 0 1rem;
  }
  .big-btn {
    padding: 0.7rem 1.2rem; border: none; border-radius: 2rem;
    font-family: inherit; font-size: 0.9rem; font-weight: 700;
    cursor: pointer; transition: all 0.2s; display: flex; align-items: center; gap: 0.4rem;
    box-shadow: var(--shadow); flex: 1; justify-content: center; max-width: 200px;
  }
  @media (min-width: 640px) {
    .big-buttons { gap: 0.75rem; margin: 1.25rem 0 1.5rem; }
    .big-btn { padding: 0.8rem 2rem; font-size: 1rem; flex: none; max-width: none; }
  }
  .big-btn:hover { transform: translateY(-1px); box-shadow: var(--shadow-lg); }
  .big-btn:active { transform: translateY(0); }
  .big-btn:disabled { opacity: 0.4; cursor: not-allowed; transform: none; box-shadow: none; }
  .big-btn.start { background: var(--green); color: #fff; }
  .big-btn.stop { background: var(--red); color: #fff; }

  /* Grid */
  .grid { display: grid; grid-template-columns: 1fr; gap: 0.75rem; margin-bottom: 1rem; }
  @media (min-width: 640px) { .grid { gap: 1rem; margin-bottom: 1.5rem; } }

  /* Cards */
  .card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 0.75rem; padding: 1rem; box-shadow: var(--shadow);
  }
  @media (min-width: 640px) { .card { border-radius: 1rem; padding: 1.25rem; } }
  .card-header {
    display: flex; align-items: center; gap: 0.5rem;
    margin-bottom: 0.75rem; cursor: pointer; user-select: none;
  }
  @media (min-width: 640px) { .card-header { margin-bottom: 1rem; } }
  .card-header .icon { font-size: 1.1rem; }
  .card-header h2 { font-size: 0.9rem; font-weight: 700; color: var(--text); flex: 1; }
  @media (min-width: 640px) {
    .card-header .icon { font-size: 1.2rem; }
    .card-header h2 { font-size: 1rem; }
  }
  .card-header .toggle-arrow { color: var(--muted); font-size: 0.8rem; transition: transform 0.2s; }
  .card-header .toggle-arrow.open { transform: rotate(180deg); }

  /* Collapsible */
  .card-body {
    overflow: hidden; transition: max-height 0.3s ease, opacity 0.3s ease, padding 0.3s ease;
    max-height: 2000px; opacity: 1;
  }
  .card-body.hidden { max-height: 0; opacity: 0; padding-top: 0; padding-bottom: 0; }

  /* Info chips */
  .info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.4rem; }
  .info-chip {
    background: var(--bg); border-radius: 0.5rem; padding: 0.5rem 0.6rem;
    display: flex; flex-direction: column;
  }
  .info-chip .chip-label { font-size: 0.65rem; color: var(--muted); font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; }
  .info-chip .chip-value { font-size: 0.8rem; font-weight: 700; margin-top: 0.1rem; word-break: break-all; }
  @media (min-width: 640px) {
    .info-grid { gap: 0.5rem; }
    .info-chip { padding: 0.6rem 0.8rem; }
    .info-chip .chip-label { font-size: 0.7rem; }
    .info-chip .chip-value { font-size: 0.9rem; }
  }

  .device-tag {
    display: inline-flex; align-items: center; gap: 0.3rem;
    background: var(--accent-soft); border-radius: 0.5rem;
    padding: 0.3rem 0.6rem; margin: 0.15rem; font-size: 0.75rem; font-weight: 600;
    color: #4a7a8f;
  }
  @media (min-width: 640px) {
    .device-tag { padding: 0.4rem 0.7rem; font-size: 0.8rem; }
  }

  /* Form elements */
  label { display: block; font-size: 0.8rem; color: var(--text); font-weight: 600; margin-bottom: 0.2rem; }
  @media (min-width: 640px) { label { font-size: 0.85rem; margin-bottom: 0.3rem; } }
  input[type="number"], select {
    width: 100%; padding: 0.5rem 0.6rem; background: var(--bg); border: 2px solid var(--border);
    border-radius: 0.6rem; color: var(--text); font-family: inherit; font-size: 16px;
    margin-bottom: 0.25rem; transition: border-color 0.2s;
  }
  input[type="number"]:focus, select:focus { outline: none; border-color: var(--accent); }

  .checkbox-row {
    display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; font-size: 0.85rem;
  }
  .checkbox-row input[type="checkbox"] {
    width: 1.2rem; height: 1.2rem; accent-color: var(--accent); margin: 0; flex-shrink: 0;
  }

  .help { font-size: 0.7rem; color: var(--muted); margin-bottom: 0.6rem; line-height: 1.4; }
  @media (min-width: 640px) { .help { font-size: 0.75rem; margin-bottom: 0.75rem; } }

  /* Sliders */
  .slider-wrap { margin-bottom: 0.25rem; }
  .slider-header { display: flex; justify-content: space-between; align-items: baseline; }
  .slider-header label { margin-bottom: 0.15rem; }
  .slider-val {
    font-size: 0.8rem; font-weight: 800; color: var(--accent);
    background: var(--accent-soft); padding: 0.1rem 0.45rem; border-radius: 0.4rem;
    min-width: 2.2rem; text-align: center; white-space: nowrap;
  }
  @media (min-width: 640px) { .slider-val { font-size: 0.85rem; min-width: 2.5rem; } }
  input[type="range"] {
    -webkit-appearance: none; appearance: none; width: 100%; height: 8px;
    background: var(--border); border-radius: 4px; outline: none;
    margin: 0.5rem 0 0.15rem; touch-action: manipulation;
  }
  input[type="range"]::-webkit-slider-thumb {
    -webkit-appearance: none; appearance: none; width: 28px; height: 28px;
    background: var(--accent); border-radius: 50%; cursor: pointer;
    border: 3px solid var(--surface); box-shadow: 0 1px 4px rgba(0,0,0,0.15);
  }
  input[type="range"]::-moz-range-thumb {
    width: 28px; height: 28px; background: var(--accent); border-radius: 50%;
    cursor: pointer; border: 3px solid var(--surface); box-shadow: 0 1px 4px rgba(0,0,0,0.15);
  }
  @media (min-width: 640px) {
    input[type="range"] { height: 6px; margin: 0.4rem 0 0.1rem; }
    input[type="range"]::-webkit-slider-thumb { width: 22px; height: 22px; }
    input[type="range"]::-moz-range-thumb { width: 22px; height: 22px; }
  }

  /* Form grid */
  .form-grid { display: grid; grid-template-columns: 1fr; gap: 0.15rem; }
  @media (min-width: 640px) { .form-grid { grid-template-columns: 1fr 1fr; gap: 0.25rem 1.25rem; } }

  /* Buttons */
  .btn {
    padding: 0.5rem 1rem; border: none; border-radius: 0.6rem;
    font-family: inherit; font-size: 0.8rem; font-weight: 700;
    cursor: pointer; transition: all 0.2s;
  }
  @media (min-width: 640px) { .btn { padding: 0.55rem 1.2rem; font-size: 0.85rem; } }
  .btn:hover { opacity: 0.85; }
  .btn-primary { background: var(--accent); color: #fff; }
  .btn-outline { background: transparent; border: 2px solid var(--border); color: var(--text); }
  .btn-outline:hover { border-color: var(--accent); color: var(--accent); }
  .btn-group { display: flex; gap: 0.5rem; margin-top: 0.75rem; flex-wrap: wrap; }

  /* Schedule */
  .schedule-item {
    background: var(--bg); border: 2px solid var(--border); border-radius: 0.75rem;
    padding: 0.6rem; margin-bottom: 0.5rem; transition: border-color 0.2s;
  }
  @media (min-width: 640px) { .schedule-item { padding: 0.75rem; } }
  .schedule-item:hover { border-color: var(--accent); }
  .schedule-item.disabled { opacity: 0.45; }
  .schedule-row {
    display: flex; align-items: center; gap: 0.4rem; flex-wrap: wrap;
  }
  @media (min-width: 640px) { .schedule-row { gap: 0.6rem; } }
  .schedule-row input[type="time"] {
    padding: 0.4rem 0.4rem; background: var(--surface); border: 2px solid var(--border);
    border-radius: 0.5rem; color: var(--text); font-family: inherit; font-size: 16px;
    font-weight: 600; width: 7rem;
  }
  .schedule-row input[type="time"]:focus { outline: none; border-color: var(--accent); }
  .time-separator { color: var(--muted); font-weight: 700; font-size: 0.85rem; }
  .day-toggles { display: flex; gap: 0.15rem; order: 10; width: 100%; margin-top: 0.4rem; justify-content: center; }
  @media (min-width: 640px) { .day-toggles { gap: 0.2rem; order: 0; width: auto; margin-top: 0; } }
  .day-btn {
    width: 2.2rem; height: 2.2rem; border-radius: 50%; border: 2px solid var(--border);
    background: var(--surface); color: var(--muted); font-size: 0.65rem; cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-weight: 800;
    font-family: inherit; transition: all 0.15s;
  }
  @media (min-width: 640px) { .day-btn { width: 2rem; height: 2rem; } }
  .day-btn:hover { border-color: var(--accent); }
  .day-btn.active { background: var(--accent); color: #fff; border-color: var(--accent); }
  .schedule-actions { display: flex; gap: 0.35rem; margin-left: auto; }
  .sched-btn {
    width: 2.2rem; height: 2.2rem; border-radius: 0.5rem; border: 2px solid var(--border);
    background: var(--surface); color: var(--muted); cursor: pointer; font-size: 0.9rem;
    display: flex; align-items: center; justify-content: center;
    font-family: inherit; transition: all 0.15s;
  }
  @media (min-width: 640px) { .sched-btn { width: 2rem; height: 2rem; } }
  .sched-btn:hover { border-color: var(--accent); color: var(--text); }
  .sched-btn.danger:hover { border-color: var(--red); color: var(--red); }

  .crontab-preview {
    font-family: 'Courier New', monospace; font-size: 0.65rem; color: var(--muted);
    background: var(--bg); border: 2px solid var(--border); border-radius: 0.5rem;
    padding: 0.4rem 0.6rem; margin-top: 0.75rem; white-space: pre-wrap; overflow-x: auto;
  }
  @media (min-width: 640px) { .crontab-preview { font-size: 0.7rem; padding: 0.5rem 0.75rem; } }

  /* Activity Log */
  .log-container {
    background: var(--bg); border: 2px solid var(--border); border-radius: 0.75rem;
    height: 250px; overflow-y: auto; padding: 0.4rem;
    font-family: 'Courier New', monospace; font-size: 0.65rem;
    -webkit-overflow-scrolling: touch;
  }
  @media (min-width: 640px) {
    .log-container { height: 300px; padding: 0.5rem; font-size: 0.75rem; }
  }
  .log-line {
    padding: 0.2rem 0.35rem; border-radius: 0.3rem; white-space: pre-wrap;
    word-break: break-all; line-height: 1.4;
  }
  .log-line:hover { background: rgba(126,184,208,0.08); }
  .log-alert { color: var(--red); font-weight: 700; background: var(--red-soft); border-radius: 0.3rem; }
  .log-quiet { color: var(--muted); }
  .log-detection { color: #3a7d3d; }
  .log-info { color: #8a6d2e; }

  .live-dot {
    display: inline-block; width: 8px; height: 8px; border-radius: 50%;
    background: var(--green); margin-right: 0.3rem;
    animation: pulse 1.5s infinite;
  }

  /* Toast */
  .toast {
    position: fixed; bottom: 1rem; left: 50%; transform: translateX(-50%) translateY(1rem);
    padding: 0.6rem 1.2rem; background: var(--text); color: var(--bg);
    border-radius: 2rem; font-weight: 700; font-size: 0.8rem;
    opacity: 0; transition: all 0.3s ease; pointer-events: none;
    box-shadow: var(--shadow-lg); max-width: 90vw; text-align: center;
  }
  .toast.show { opacity: 1; transform: translateX(-50%) translateY(0); }
  .toast.error { background: var(--red); color: #fff; }

  /* Charts */
  .chart-tabs {
    display: flex; gap: 0.25rem; margin-bottom: 0.75rem; justify-content: center;
  }
  .chart-tab {
    padding: 0.45rem 1rem; border: 2px solid var(--border); border-radius: 2rem;
    background: var(--surface); color: var(--muted); font-family: inherit;
    font-size: 0.8rem; font-weight: 700; cursor: pointer; transition: all 0.15s;
  }
  .chart-tab.active { background: var(--accent); color: #fff; border-color: var(--accent); }
  .chart-tab:hover:not(.active) { border-color: var(--accent); }

  .chart-area {
    background: var(--bg); border: 2px solid var(--border); border-radius: 0.75rem;
    padding: 0.5rem; position: relative; height: 220px;
  }
  @media (min-width: 640px) { .chart-area { padding: 0.75rem; } }
  .chart-scroll { min-width: 100%; }
  .chart-bars {
    display: flex; align-items: flex-end; gap: 1px; height: 120px;
  }
  @media (min-width: 640px) { .chart-bars { height: 180px; gap: 3px; } }
  .chart-col {
    flex: 1; min-width: 10px; display: flex; flex-direction: column; align-items: center;
    justify-content: flex-end; height: 100%; position: relative;
  }
  .chart-bar-stack { width: 100%; display: flex; flex-direction: column-reverse; }
  .chart-bar-crying {
    background: var(--red); border-radius: 2px 2px 0 0; min-height: 0;
    transition: height 0.3s ease;
  }
  .chart-bar-ambient {
    background: var(--green); border-radius: 0; min-height: 0;
    transition: height 0.3s ease;
  }
  .chart-bar-alert {
    background: var(--yellow); border-radius: 2px 2px 0 0; min-height: 0;
    transition: height 0.3s ease;
  }
  .chart-label {
    font-size: 0.5rem; color: var(--muted); margin-top: 0.15rem;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    max-width: 100%; text-align: center;
  }
  @media (min-width: 640px) { .chart-label { font-size: 0.6rem; } }
  .chart-legend {
    display: flex; gap: 0.75rem; justify-content: center; margin-top: 0.5rem; font-size: 0.65rem;
  }
  @media (min-width: 640px) { .chart-legend { font-size: 0.7rem; gap: 1rem; } }
  .chart-legend-item { display: flex; align-items: center; gap: 0.25rem; color: var(--muted); font-weight: 600; }
  .chart-legend-dot { width: 8px; height: 8px; border-radius: 2px; flex-shrink: 0; }

  .stats-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 0.4rem; margin-bottom: 0.75rem; }
  @media (min-width: 640px) { .stats-grid { grid-template-columns: repeat(4, 1fr); gap: 0.5rem; } }
  .stat-card {
    background: var(--bg); border-radius: 0.6rem; padding: 0.5rem; text-align: center;
  }
  .stat-number { font-size: 1.3rem; font-weight: 800; color: var(--text); }
  @media (min-width: 640px) { .stat-number { font-size: 1.5rem; } }
  .stat-label { font-size: 0.6rem; color: var(--muted); font-weight: 600; text-transform: uppercase; }
  @media (min-width: 640px) { .stat-label { font-size: 0.65rem; } }

  .alert-list { max-height: 150px; overflow-y: auto; -webkit-overflow-scrolling: touch; }
  .alert-item {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.5rem; border-radius: 0.4rem; font-size: 0.8rem;
    border-bottom: 1px solid var(--border);
  }
  @media (min-width: 640px) { .alert-item { padding: 0.35rem 0.5rem; font-size: 0.75rem; } }
  .alert-item:last-child { border-bottom: none; }
  .alert-time { color: var(--red); font-weight: 700; }
  .alert-date { color: var(--muted); font-size: 0.7rem; }
</style>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4/dist/chart.umd.min.js"></script>
</head>
<body>
<div class="container">
  <header>
    <div class="header-icon">&#x1F476;</div>
    <h1>Baby Monitor</h1>
    <div class="subtitle" id="headerSubtitle">Keeping an ear on your little one</div>
  </header>

  <div id="statusBar" class="status-bar sleeping">
    <div class="status-dot"></div>
    <span id="statusText">Monitor is off</span>
  </div>

  <div class="big-buttons">
    <button class="big-btn start" id="btnStart" onclick="startDetector()">&#x1F50A; Start Listening</button>
    <button class="big-btn stop" id="btnStop" onclick="stopDetector()" disabled>&#x23F8; Stop</button>
  </div>

  <div class="grid">

    <!-- Schedule -->
    <div class="card">
      <div class="card-header" onclick="toggleCard('schedule')">
        <span class="icon">&#x1F319;</span>
        <h2>Bedtime Schedule</h2>
        <span class="toggle-arrow open" id="arrow-schedule">&#x25BC;</span>
      </div>
      <div class="card-body" id="body-schedule">
        <p class="help">Set when to automatically start and stop listening. Perfect for nap time and bedtime routines.</p>
        <div id="scheduleStatus" style="display:none; background:var(--green-soft); border-radius:0.6rem; padding:0.6rem 0.9rem; margin-bottom:0.75rem; font-size:0.85rem; font-weight:600; color:#3a7d3d;"></div>
        <div id="scheduleList"></div>
        <div class="btn-group">
          <button class="btn btn-outline" onclick="addSchedule()">+ Add Time</button>
          <button class="btn btn-primary" onclick="saveConfig()">&#x1F4BE; Save Schedule</button>
        </div>
        <div id="crontabPreview" class="crontab-preview" style="display:none;"></div>
      </div>
    </div>

    <!-- Activity Log -->
    <div class="card">
      <div class="card-header" onclick="toggleCard('log')">
        <span class="icon">&#x1F4CB;</span>
        <h2>Activity <span id="liveIndicator"></span></h2>
        <span class="toggle-arrow open" id="arrow-log">&#x25BC;</span>
      </div>
      <div class="card-body" id="body-log">
        <div class="log-container" id="logContainer">
          <div class="log-line log-quiet">Waiting to start...</div>
        </div>
      </div>
    </div>

    <!-- History & Charts -->
    <div class="card">
      <div class="card-header" onclick="toggleCard('history')">
        <span class="icon">&#x1F4CA;</span>
        <h2>History</h2>
        <span class="toggle-arrow open" id="arrow-history">&#x25BC;</span>
      </div>
      <div class="card-body" id="body-history">
        <div class="stats-grid" id="statsGrid">
          <div class="stat-card"><div class="stat-number" id="statAlerts">-</div><div class="stat-label">Alerts today</div></div>
          <div class="stat-card"><div class="stat-number" id="statCrying">-</div><div class="stat-label">Cry detections</div></div>
          <div class="stat-card"><div class="stat-number" id="statTotal">-</div><div class="stat-label">Total checks</div></div>
          <div class="stat-card"><div class="stat-number" id="statCryPct">-</div><div class="stat-label">Cry rate</div></div>
        </div>

        <div class="chart-tabs" id="chartTabs">
          <button class="chart-tab active" onclick="loadChart('today', this)">Today</button>
          <button class="chart-tab" onclick="loadChart('week', this)">Week</button>
          <button class="chart-tab" onclick="loadChart('month', this)">Month</button>
        </div>
        <div class="chart-area">
          <canvas id="chartCanvas" style="width:100%; height:200px;"></canvas>
        </div>

        <div style="margin-top:0.75rem;">
          <label>Recent Alerts</label>
          <div class="alert-list" id="alertList">
            <div class="alert-item"><span class="alert-date">No alerts yet</span></div>
          </div>
        </div>
      </div>
    </div>

    <!-- Sensitivity Settings -->
    <div class="card">
      <div class="card-header" onclick="toggleCard('settings')">
        <span class="icon">&#x2699;&#xFE0F;</span>
        <h2>Sensitivity Settings</h2>
        <span class="toggle-arrow" id="arrow-settings">&#x25BC;</span>
      </div>
      <div class="card-body hidden" id="body-settings">
        <p class="help" style="margin-bottom:1rem;">Fine-tune how the monitor detects crying. The defaults work well for most rooms.</p>
        <div style="margin-bottom:1rem;">
          <label for="mic_device">&#x1F3A4; Microphone</label>
          <div style="display:flex; gap:0.5rem; align-items:center;">
            <select id="mic_device" style="flex:1;">
              <option value="">Auto-detect</option>
            </select>
            <button class="btn btn-outline" onclick="loadDevices()" title="Refresh devices" style="padding:0.55rem 0.7rem;">&#x1F504;</button>
          </div>
          <p class="help">Choose which microphone to use for listening.</p>
        </div>
        <div class="form-grid">
          <div>
            <div class="slider-wrap">
              <div class="slider-header"><label>How often to check</label><span class="slider-val" id="interval_val">5s</span></div>
              <input type="range" id="interval" min="1" max="30" step="0.5" oninput="$('interval_val').textContent=this.value+'s'">
            </div>
            <p class="help">Seconds between each audio check. Lower means faster response.</p>
          </div>
          <div>
            <div class="slider-wrap">
              <div class="slider-header"><label>Microphone boost</label><span class="slider-val" id="amplification_val">10x</span></div>
              <input type="range" id="amplification" min="1" max="40" step="0.5" oninput="$('amplification_val').textContent=this.value+'x'">
            </div>
            <p class="help">Increase if the microphone is far from the crib.</p>
          </div>
          <div>
            <div class="slider-wrap">
              <div class="slider-header"><label>Noise floor</label><span class="slider-val" id="min_contrast_val">15 dB</span></div>
              <input type="range" id="min_contrast" min="0" max="40" step="0.5" oninput="$('min_contrast_val').textContent=this.value+' dB'">
            </div>
            <p class="help">Ignores sounds quieter than this above background noise.</p>
          </div>
          <div>
            <div class="slider-wrap">
              <div class="slider-header"><label>Checks before alert</label><span class="slider-val" id="consecutive_val">6</span></div>
              <input type="range" id="consecutive" min="1" max="20" step="1" oninput="$('consecutive_val').textContent=this.value">
            </div>
            <p class="help">How many consecutive crying detections before alerting you.</p>
          </div>
          <div>
            <div class="slider-wrap">
              <div class="slider-header"><label>Alert sensitivity</label><span class="slider-val" id="fraction_val">50%</span></div>
              <input type="range" id="fraction" min="0" max="1" step="0.05" oninput="$('fraction_val').textContent=Math.round(this.value*100)+'%'">
            </div>
            <p class="help">0% = very sensitive, 100% = only alert on sustained crying.</p>
          </div>
          <div>
            <div class="slider-wrap">
              <div class="slider-header"><label>Detection confidence</label><span class="slider-val" id="prob_threshold_val">80%</span></div>
              <input type="range" id="prob_threshold" min="0.1" max="1" step="0.05" oninput="$('prob_threshold_val').textContent=Math.round(this.value*100)+'%'">
            </div>
            <p class="help">How sure the AI must be. Lower may cause false alarms.</p>
          </div>
          <div>
            <div class="slider-wrap">
              <div class="slider-header"><label>Alert cooldown</label><span class="slider-val" id="cooldown_val">3 min</span></div>
              <input type="range" id="cooldown" min="0" max="1800" step="30" oninput="$('cooldown_val').textContent=this.value>=60?Math.round(this.value/60)+' min':this.value+'s'">
            </div>
            <p class="help">Wait time after an alert before alerting again.</p>
          </div>
          <div style="display:flex; flex-direction:column; justify-content:center; padding-top:0.3rem;">
            <div class="checkbox-row">
              <input type="checkbox" id="threshold_only">
              <label for="threshold_only" style="margin:0;">Simple mode (any loud sound)</label>
            </div>
            <p class="help">Skip AI detection and alert on any loud noise. Less accurate but simpler.</p>
          </div>
        </div>
        <div style="margin-top:1rem; text-align:center; display:flex; gap:0.5rem; justify-content:center; flex-wrap:wrap;">
          <button class="big-btn start" onclick="saveConfig()" style="background:var(--accent);">&#x1F4BE; Save Settings</button>
          <button class="big-btn" onclick="resetConfig()" style="background:var(--muted); color:#fff; font-size:0.9rem;">&#x21BA; Reset Defaults</button>
        </div>
      </div>
    </div>

    <!-- Device & System Info -->
    <div class="card">
      <div class="card-header" onclick="toggleCard('system')">
        <span class="icon">&#x1F4E1;</span>
        <h2>Device Info</h2>
        <span class="toggle-arrow" id="arrow-system">&#x25BC;</span>
      </div>
      <div class="card-body hidden" id="body-system">
        <div class="info-grid" id="systemInfo">
          <div class="info-chip"><span class="chip-label">Loading</span><span class="chip-value">...</span></div>
        </div>
        <div style="margin-top:0.75rem;">
          <label style="margin-bottom:0.4rem;">Microphones</label>
          <div id="deviceList"><span class="help">Scanning...</span></div>
        </div>
        <div style="margin-top:1.25rem; padding-top:0.75rem; border-top:2px solid var(--border); display:flex; gap:0.5rem; flex-wrap:wrap;">
          <button class="btn" onclick="systemAction('reboot')" style="background:var(--yellow);color:var(--text);">&#x1F504; Reboot</button>
          <button class="btn" onclick="systemAction('shutdown')" style="background:var(--red);color:#fff;">&#x23FB; Shutdown</button>
        </div>
      </div>
    </div>

  </div>
</div>

<div class="toast" id="toast"></div>

<script>
const $ = id => document.getElementById(id);

// --- Card collapse ---
function toggleCard(name) {
  const body = $('body-' + name);
  const arrow = $('arrow-' + name);
  body.classList.toggle('hidden');
  arrow.classList.toggle('open');
}

// --- Toast ---
function showToast(msg, isError) {
  const t = $('toast');
  t.textContent = msg;
  t.className = 'toast show' + (isError ? ' error' : '');
  setTimeout(() => t.className = 'toast', 2500);
}

// --- API ---
async function api(path, opts) {
  const r = await fetch('/api/' + path, opts);
  const data = await r.json();
  if (!r.ok) throw new Error(data.error || 'Something went wrong');
  return data;
}

// --- System Info ---
async function loadSystemInfo() {
  try {
    const info = await api('system');
    const el = $('systemInfo');
    const items = [
      { label: 'Name', value: info.hostname },
      { label: 'Address', value: info.ip },
      { label: 'Temperature', value: info.cpu_temp },
      { label: 'Up for', value: info.uptime },
      { label: 'Memory', value: info.memory },
      { label: 'Disk', value: info.disk },
      { label: 'Runs in', value: info.environment },
    ];
    el.innerHTML = '';
    items.filter(i => i.value).forEach(i => {
      const chip = document.createElement('div');
      chip.className = 'info-chip';
      const lbl = document.createElement('span');
      lbl.className = 'chip-label';
      lbl.textContent = i.label;
      const val = document.createElement('span');
      val.className = 'chip-value';
      val.textContent = i.value;
      chip.append(lbl, val);
      el.appendChild(chip);
    });
  } catch(e) {}
}

// --- Devices ---
async function loadDevices() {
  try {
    const data = await api('devices');
    const el = $('deviceList');
    const sel = $('mic_device');
    const capture = data.capture || [];

    // Populate device info section (textContent — device names are untrusted)
    el.innerHTML = '';
    if (capture.length === 0) {
      el.innerHTML = '<span class="help">No microphones detected</span>';
    } else {
      for (const d of capture) {
        const tag = document.createElement('div');
        tag.className = 'device-tag';
        tag.textContent = '\u{1F3A4} ' + (d.label || d.card);
        el.appendChild(tag);
      }
    }

    // Populate mic selector (keep current selection)
    const current = sel.value;
    sel.innerHTML = '<option value="">Auto-detect</option>';
    for (const d of capture) {
      const opt = document.createElement('option');
      opt.value = d.id;
      opt.textContent = (d.label || d.card) + (d.id.startsWith('hw:') ? ' (' + d.id + ')' : '');
      sel.appendChild(opt);
    }
    if (current) sel.value = current;
  } catch(e) {}
}

// --- Config ---
let schedules = [];

async function loadConfig() {
  try {
    const cfg = await api('config');
    $('interval').value = cfg.interval;
    $('amplification').value = cfg.amplification;
    $('min_contrast').value = cfg.min_contrast;
    $('consecutive').value = cfg.consecutive;
    $('fraction').value = cfg.fraction;
    $('prob_threshold').value = cfg.prob_threshold;
    $('cooldown').value = cfg.cooldown;
    $('threshold_only').checked = cfg.threshold_only;
    $('mic_device').value = cfg.mic_device || '';
    // Update slider display values
    $('interval_val').textContent = cfg.interval + 's';
    $('amplification_val').textContent = cfg.amplification + 'x';
    $('min_contrast_val').textContent = cfg.min_contrast + ' dB';
    $('consecutive_val').textContent = cfg.consecutive;
    $('fraction_val').textContent = Math.round(cfg.fraction * 100) + '%';
    $('prob_threshold_val').textContent = Math.round(cfg.prob_threshold * 100) + '%';
    const cd = cfg.cooldown;
    $('cooldown_val').textContent = cd >= 60 ? Math.round(cd / 60) + ' min' : cd + 's';
    schedules = cfg.schedules || [];
    renderSchedules();
  } catch(e) {}
}

async function saveConfig() {
  try {
    const cfg = {
      interval: parseFloat($('interval').value),
      amplification: parseFloat($('amplification').value),
      min_contrast: parseFloat($('min_contrast').value),
      consecutive: parseInt($('consecutive').value),
      fraction: parseFloat($('fraction').value),
      prob_threshold: parseFloat($('prob_threshold').value),
      cooldown: parseInt($('cooldown').value),
      threshold_only: $('threshold_only').checked,
      mic_device: $('mic_device').value,
      schedules: schedules,
    };
    await api('config', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(cfg) });
    showToast('Settings saved!');
    loadScheduleStatus();
  } catch(e) {
    showToast('Could not save: ' + e.message, true);
  }
}

async function resetConfig() {
  if (!confirm('Reset all settings to defaults?')) return;
  try {
    const cfg = await api('config/reset', { method: 'POST' });
    loadConfig();
    showToast('Settings reset to defaults!');
  } catch(e) {
    showToast('Could not reset: ' + e.message, true);
  }
}

// --- Schedule ---
const DAY_LABELS = ['S','M','T','W','T','F','S'];
const DAY_NAMES = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'];

function addSchedule() {
  schedules.push({ enabled: true, start: '21:00', stop: '07:00', days: [] });
  renderSchedules();
}

function removeSchedule(i) {
  schedules.splice(i, 1);
  renderSchedules();
}

function toggleSchedule(i) {
  schedules[i].enabled = !schedules[i].enabled;
  renderSchedules();
}

function toggleDay(si, day) {
  const s = schedules[si];
  const idx = s.days.indexOf(day);
  if (idx >= 0) s.days.splice(idx, 1);
  else s.days.push(day);
  s.days.sort();
  renderSchedules();
}

function updateScheduleTime(si, field, val) {
  schedules[si][field] = val;
}

function renderSchedules() {
  const el = $('scheduleList');
  if (schedules.length === 0) {
    el.innerHTML = '<p class="help" style="padding:0.5rem 0;">No schedules set. Tap "+ Add Time" to create one.</p>';
    return;
  }
  el.innerHTML = schedules.map((s, i) => {
    const cls = s.enabled ? '' : ' disabled';
    const daysHtml = DAY_LABELS.map((label, d) => {
      const active = s.days.length === 0 || s.days.includes(d) ? ' active' : '';
      return '<button class="day-btn' + active + '" onclick="toggleDay(' + i + ',' + d + ')" title="' + DAY_NAMES[d] + '">' + label + '</button>';
    }).join('');

    return '<div class="schedule-item' + cls + '">' +
      '<div class="schedule-row">' +
        '<input type="time" value="' + s.start + '" onchange="updateScheduleTime(' + i + ',\'start\',this.value)">' +
        '<span class="time-separator">&#x27A1;</span>' +
        '<input type="time" value="' + s.stop + '" onchange="updateScheduleTime(' + i + ',\'stop\',this.value)">' +
        '<div class="day-toggles">' + daysHtml + '</div>' +
        '<div class="schedule-actions">' +
          '<button class="sched-btn" onclick="toggleSchedule(' + i + ')" title="' + (s.enabled ? 'Pause' : 'Resume') + '">' + (s.enabled ? '&#x23F8;' : '&#x25B6;&#xFE0F;') + '</button>' +
          '<button class="sched-btn danger" onclick="removeSchedule(' + i + ')" title="Remove">&#x2715;</button>' +
        '</div>' +
      '</div></div>';
  }).join('');
}

async function loadScheduleStatus() {
  try {
    const data = await api('schedule/status');
    const el = $('crontabPreview');
    const status = $('scheduleStatus');

    if (data.schedules && data.schedules.length > 0) {
      // Find next event
      const now = data.current_time;
      let nextStart = null, nextStop = null;
      for (const s of data.schedules) {
        if (!s.enabled) continue;
        if (!nextStart || s.start > now) nextStart = s.start;
        if (!nextStop || s.stop > now) nextStop = s.stop;
      }

      // Determine if we're currently in a schedule window
      const running = $('btnStart').disabled; // detector is running
      if (running) {
        status.style.display = 'block';
        status.style.background = 'var(--green-soft)';
        status.style.color = '#3a7d3d';
        status.innerHTML = '&#x2705; Schedule active &mdash; listening until ' + (nextStop || '?') + ' &nbsp;(current time: ' + data.current_time + ')';
      } else {
        status.style.display = 'block';
        status.style.background = 'var(--lavender-soft)';
        status.style.color = '#6b5b8d';
        status.innerHTML = '&#x1F319; Next listen starts at ' + (nextStart || '?') + ' &nbsp;(current time: ' + data.current_time + ')';
      }
    } else {
      status.style.display = 'none';
    }

    if (data.crontab) {
      el.style.display = 'block';
      el.textContent = data.crontab;
    } else {
      el.style.display = 'none';
    }
  } catch(e) {}
}

// --- Detector ---
let isRunning = false;

async function startDetector() {
  try {
    await api('detector/start', { method: 'POST' });
    updateStatus(true);
    connectSSE();
    showToast('Listening started!');
    loadScheduleStatus();
  } catch(e) {
    showToast(e.message, true);
  }
}

async function stopDetector() {
  try {
    await api('detector/stop', { method: 'POST' });
    updateStatus(false);
    showToast('Monitor stopped');
    loadScheduleStatus();
  } catch(e) {
    showToast(e.message, true);
  }
}

function updateStatus(running) {
  isRunning = running;
  const bar = $('statusBar');
  const text = $('statusText');
  bar.className = 'status-bar ' + (running ? 'listening' : 'sleeping');
  text.textContent = running ? 'Listening for your baby...' : 'Monitor is off';

  $('btnStart').disabled = running;
  $('btnStop').disabled = !running;

  const indicator = $('liveIndicator');
  indicator.innerHTML = running ? '<span class="live-dot"></span>Live' : '';
}

async function checkStatus() {
  try {
    const data = await api('detector/status');
    updateStatus(data.running);
    if (data.logs && data.logs.length > 0) {
      const container = $('logContainer');
      container.innerHTML = '';
      for (const log of data.logs) appendLog(log);
    }
    if (data.running) connectSSE();
  } catch(e) {}
}

function appendLog(entry) {
  const container = $('logContainer');
  // Remove placeholder
  if (container.children.length === 1 && container.children[0].textContent === 'Waiting to start...') {
    container.innerHTML = '';
  }
  const div = document.createElement('div');
  div.className = 'log-line log-' + entry.level;
  div.textContent = entry.time + '  ' + entry.message;
  container.appendChild(div);
  container.scrollTop = container.scrollHeight;
}

let evtSource = null;
function connectSSE() {
  if (evtSource) evtSource.close();
  evtSource = new EventSource('/api/detector/stream');
  evtSource.onmessage = (e) => {
    const entry = JSON.parse(e.data);
    appendLog(entry);
  };
  evtSource.onerror = () => {
    setTimeout(() => { if (isRunning) connectSSE(); }, 3000);
  };
}

// --- System Actions ---
async function systemAction(action) {
  const label = action === 'reboot' ? 'reboot' : 'shut down';
  if (!confirm('Are you sure you want to ' + label + ' the monitor?')) return;
  try {
    await api('system/' + action, { method: 'POST' });
    showToast(action === 'reboot' ? 'Rebooting...' : 'Shutting down...');
  } catch(e) {
    showToast(e.message, true);
  }
}

// --- History & Charts ---
let currentChartView = 'today';

async function loadStats() {
  try {
    const stats = await api('history/stats');
    $('statAlerts').textContent = stats.alerts;
    $('statCrying').textContent = stats.cry_count;
    $('statTotal').textContent = stats.detections;
    $('statCryPct').textContent = stats.detections > 0 ? Math.round(stats.cry_percent) + '%' : '-';
  } catch(e) {}
}

async function loadChart(view, el) {
  currentChartView = view;
  document.querySelectorAll('.chart-tab').forEach(t => t.classList.remove('active'));
  if (el) el.classList.add('active');

  try {
    const data = await api('history/chart?view=' + view);
    renderChart(data, view);
  } catch(e) {}
}

async function loadChartDefault() {
  try {
    const data = await api('history/chart?view=today');
    renderChart(data, 'today');
  } catch(e) {}
}

let chartInstance = null;

function renderChart(data, view) {
  const canvas = $('chartCanvas');
  if (!data || data.length === 0) {
    if (chartInstance) { chartInstance.destroy(); chartInstance = null; }
    canvas.parentElement.innerHTML = '<canvas id="chartCanvas" style="width:100%;height:200px;"></canvas><div style="display:flex;align-items:center;justify-content:center;width:100%;height:200px;color:var(--muted);font-size:0.85rem;">No data yet</div>';
    return;
  }

  if (chartInstance) { chartInstance.destroy(); }

  const labels = data.map(d => {
    if (view === 'today') return d.label.replace(':00', 'h');
    const parts = d.label.split('-');
    return parts.length === 3 ? parts[2] + '/' + parts[1] : d.label;
  });

  // Use CSS variables from the baby theme
  const style = getComputedStyle(document.documentElement);
  const red = style.getPropertyValue('--red').trim() || '#e07070';
  const redSoft = style.getPropertyValue('--red-soft').trim() || '#fde8e8';
  const pink = style.getPropertyValue('--pink').trim() || '#e8a0b4';
  const pinkSoft = style.getPropertyValue('--pink-soft').trim() || '#fce4ec';
  const green = style.getPropertyValue('--green').trim() || '#7bc67e';
  const greenSoft = style.getPropertyValue('--green-soft').trim() || '#e8f5e9';
  const accent = style.getPropertyValue('--accent').trim() || '#7eb8d0';
  const muted = style.getPropertyValue('--muted').trim() || '#9ca3af';
  const border = style.getPropertyValue('--border').trim() || '#e8e0f0';

  chartInstance = new Chart(canvas, {
    type: 'bar',
    data: {
      labels: labels,
      datasets: [
        {
          label: 'Alerts',
          data: data.map(d => d.alerts),
          backgroundColor: redSoft,
          borderColor: red,
          borderWidth: 1.5,
          borderRadius: 4,
          yAxisID: 'y',
          order: 2
        },
        {
          label: 'Crying',
          type: 'line',
          data: data.map(d => d.crying),
          borderColor: pink,
          backgroundColor: pinkSoft + '80',
          borderWidth: 2.5,
          pointRadius: 3,
          pointBackgroundColor: pink,
          pointBorderColor: '#fff',
          pointBorderWidth: 1.5,
          pointHoverRadius: 5,
          tension: 0.35,
          fill: true,
          yAxisID: 'y1',
          order: 1
        },
        {
          label: 'Quiet',
          type: 'line',
          data: data.map(d => d.ambient),
          borderColor: border,
          backgroundColor: 'transparent',
          borderWidth: 1,
          pointRadius: 0,
          pointHoverRadius: 3,
          tension: 0.35,
          fill: false,
          yAxisID: 'y1',
          order: 0,
          borderDash: [4, 3]
        }
      ]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: {
          position: 'bottom',
          labels: { boxWidth: 10, padding: 12, font: { size: 11, family: 'Nunito, sans-serif' }, usePointStyle: true, pointStyle: 'circle' }
        },
        tooltip: { mode: 'index', intersect: false, backgroundColor: '#fff', titleColor: '#333', bodyColor: '#555', borderColor: border, borderWidth: 1, padding: 10, cornerRadius: 8, titleFont: { weight: '700', family: 'Nunito' }, bodyFont: { family: 'Nunito' } }
      },
      scales: {
        x: {
          ticks: { maxRotation: 0, autoSkip: true, maxTicksLimit: view === 'today' ? 12 : 10, font: { size: 9, family: 'Nunito' }, color: muted },
          grid: { display: false },
          border: { display: false }
        },
        y: {
          position: 'left',
          title: { display: true, text: 'Alerts', font: { size: 9, family: 'Nunito', weight: '600' }, color: red },
          beginAtZero: true,
          ticks: { precision: 0, font: { size: 9 }, color: red + 'cc' },
          grid: { color: border + '60', lineWidth: 0.5 },
          border: { display: false }
        },
        y1: {
          position: 'right',
          title: { display: true, text: 'Detections', font: { size: 9, family: 'Nunito', weight: '600' }, color: muted },
          beginAtZero: true,
          ticks: { precision: 0, font: { size: 9 }, color: muted },
          grid: { drawOnChartArea: false },
          border: { display: false }
        }
      },
      interaction: { mode: 'index', intersect: false }
    }
  });
}

async function loadAlerts() {
  try {
    const alerts = await api('history/alerts?limit=20');
    const el = $('alertList');
    if (!alerts || alerts.length === 0) {
      el.innerHTML = '<div class="alert-item"><span class="alert-date">No alerts yet</span></div>';
      return;
    }
    el.innerHTML = alerts.map(a => {
      const ts = a.timestamp.replace('T', ' ').substring(0, 16);
      const parts = ts.split(' ');
      const date = parts[0] || '';
      const time = parts[1] || ts;
      return '<div class="alert-item">' +
        '<span class="alert-time">' + time + '</span>' +
        '<span class="alert-date">' + date + '</span>' +
      '</div>';
    }).join('');
  } catch(e) {}
}

async function loadHistory() {
  loadStats();
  loadChartDefault();
  loadAlerts();
}

// --- Init ---
async function init() {
  loadSystemInfo();
  await loadDevices();
  await loadConfig();
  await checkStatus();
  loadScheduleStatus();
  loadHistory();
}
init();

setInterval(loadSystemInfo, 30000);
setInterval(loadScheduleStatus, 30000);
setInterval(loadStats, 30000);

// Poll detector status every 5s to catch cron-triggered start/stop
setInterval(async () => {
  try {
    const data = await api('detector/status');
    const wasRunning = isRunning;
    updateStatus(data.running);
    if (data.running && !wasRunning) {
      // Just started (likely by cron)
      connectSSE();
      loadScheduleStatus();
      const container = $('logContainer');
      container.innerHTML = '';
      if (data.logs) for (const log of data.logs) appendLog(log);
    } else if (!data.running && wasRunning) {
      // Just stopped (likely by cron)
      loadScheduleStatus();
    }
  } catch(e) {}
}, 5000);
</script>
</body>
</html>`
