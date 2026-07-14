package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDB() error {
	dataDir := os.Getenv("BABY_MONITOR_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(homeDir, "babymonitor")
	}
	dbPath := filepath.Join(dataDir, "babymonitor.db")
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	// WAL mode for better concurrent reads
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			type TEXT NOT NULL,
			message TEXT,
			probability REAL,
			classification TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
		CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);

		CREATE TABLE IF NOT EXISTS state (
			key TEXT PRIMARY KEY,
			value TEXT
		);
	`)
	if err != nil {
		return err
	}

	// Prune old records (keep 30 days)
	_, err = db.Exec(`DELETE FROM events WHERE timestamp < datetime('now', 'localtime', '-30 days')`)
	return err
}

// Event types: "alert", "crying", "ambient", "babbling", "start", "stop"

func logEvent(eventType, message string, probability float64, classification string) {
	if db == nil {
		return
	}
	_, err := db.Exec(
		`INSERT INTO events (timestamp, type, message, probability, classification) VALUES (?, ?, ?, ?, ?)`,
		time.Now().Format("2006-01-02 15:04:05"),
		eventType, message, probability, classification,
	)
	if err != nil {
		log.Printf("DB insert error: %v", err)
	}
}

type EventSummary struct {
	Date       string `json:"date"`
	Alerts     int    `json:"alerts"`
	CryCount   int    `json:"cry_count"`
	TotalCount int    `json:"total_count"`
}

type RecentEvent struct {
	ID             int     `json:"id"`
	Timestamp      string  `json:"timestamp"`
	Type           string  `json:"type"`
	Message        string  `json:"message"`
	Probability    float64 `json:"probability"`
	Classification string  `json:"classification"`
}

func getRecentEvents(limit int) ([]RecentEvent, error) {
	rows, err := db.Query(
		`SELECT id, timestamp, type, message, probability, classification 
		 FROM events ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []RecentEvent
	for rows.Next() {
		var e RecentEvent
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Type, &e.Message, &e.Probability, &e.Classification); err != nil {
			continue
		}
		events = append(events, e)
	}
	return events, nil
}

// --- Persistent State ---

func setState(key, value string) {
	if db == nil {
		return
	}
	db.Exec(`INSERT INTO state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=?`, key, value, value)
}

func getState(key string) string {
	if db == nil {
		return ""
	}
	var val string
	row := db.QueryRow(`SELECT value FROM state WHERE key=?`, key)
	if err := row.Scan(&val); err != nil {
		return ""
	}
	return val
}

func setDesiredRunning(running bool) {
	if running {
		setState("desired_state", "running")
	} else {
		setState("desired_state", "stopped")
	}
}

func getDesiredRunning() bool {
	return getState("desired_state") == "running"
}

func getDailySummary(days int) ([]EventSummary, error) {
	rows, err := db.Query(`
		SELECT date(timestamp) as d,
			COALESCE(SUM(CASE WHEN type='alert' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type='crying' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type IN ('crying','ambient','babbling') THEN 1 ELSE 0 END), 0)
		FROM events
		WHERE timestamp >= datetime('now', 'localtime', ?)
		GROUP BY d ORDER BY d`, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []EventSummary
	for rows.Next() {
		var s EventSummary
		if err := rows.Scan(&s.Date, &s.Alerts, &s.CryCount, &s.TotalCount); err != nil {
			continue
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

type TodayStats struct {
	Alerts      int     `json:"alerts"`
	CryCount    int     `json:"cry_count"`
	Detections  int     `json:"detections"`
	CryPercent  float64 `json:"cry_percent"`
	LastAlert   string  `json:"last_alert"`
	UptimeHours float64 `json:"uptime_hours"`
}

func getTodayStats() (TodayStats, error) {
	var stats TodayStats

	row := db.QueryRow(`
		SELECT 
			COALESCE(SUM(CASE WHEN type='alert' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type='crying' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type IN ('crying','ambient','babbling') THEN 1 ELSE 0 END), 0)
		FROM events WHERE date(timestamp) = date('now', 'localtime')
	`)
	if err := row.Scan(&stats.Alerts, &stats.CryCount, &stats.Detections); err != nil {
		return stats, err
	}

	if stats.Detections > 0 {
		stats.CryPercent = float64(stats.CryCount) / float64(stats.Detections) * 100
	}

	// Last alert time
	row = db.QueryRow(`SELECT timestamp FROM events WHERE type='alert' ORDER BY timestamp DESC LIMIT 1`)
	var lastAlert sql.NullString
	row.Scan(&lastAlert)
	if lastAlert.Valid {
		stats.LastAlert = lastAlert.String
	}

	// Uptime: time between first start and last event today
	row = db.QueryRow(`
		SELECT 
			COALESCE((julianday(MAX(timestamp)) - julianday(MIN(timestamp))) * 24, 0)
		FROM events WHERE date(timestamp) = date('now', 'localtime')
	`)
	row.Scan(&stats.UptimeHours)

	return stats, nil
}

// ChartPoint is a single data point for charts
type ChartPoint struct {
	Label    string `json:"label"`
	Crying   int    `json:"crying"`
	Ambient  int    `json:"ambient"`
	Alerts   int    `json:"alerts"`
}

// getHourlyChart returns hourly breakdown for a given date
func getHourlyChart(date string) ([]ChartPoint, error) {
	rows, err := db.Query(`
		SELECT 
			strftime('%H', timestamp) as hour,
			SUM(CASE WHEN type='crying' THEN 1 ELSE 0 END),
			SUM(CASE WHEN type IN ('ambient','babbling') THEN 1 ELSE 0 END),
			SUM(CASE WHEN type='alert' THEN 1 ELSE 0 END)
		FROM events 
		WHERE date(timestamp) = ? AND type IN ('crying','ambient','babbling','alert')
		GROUP BY hour ORDER BY hour
	`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Fill all 24 hours
	hourData := make(map[string]ChartPoint)
	for rows.Next() {
		var h string
		var p ChartPoint
		if err := rows.Scan(&h, &p.Crying, &p.Ambient, &p.Alerts); err != nil {
			continue
		}
		hourData[h] = p
	}

	var points []ChartPoint
	for i := 0; i < 24; i++ {
		h := fmt.Sprintf("%02d", i)
		p := hourData[h]
		p.Label = h + ":00"
		points = append(points, p)
	}
	return points, nil
}

// getDailyChart returns daily breakdown for the last N days
func getDailyChart(days int) ([]ChartPoint, error) {
	rows, err := db.Query(`
		SELECT 
			date(timestamp) as day,
			SUM(CASE WHEN type='crying' THEN 1 ELSE 0 END),
			SUM(CASE WHEN type IN ('ambient','babbling') THEN 1 ELSE 0 END),
			SUM(CASE WHEN type='alert' THEN 1 ELSE 0 END)
		FROM events 
		WHERE timestamp >= datetime('now', 'localtime', ? || ' days')
		AND type IN ('crying','ambient','babbling','alert')
		GROUP BY day ORDER BY day
	`, -days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []ChartPoint
	for rows.Next() {
		var p ChartPoint
		if err := rows.Scan(&p.Label, &p.Crying, &p.Ambient, &p.Alerts); err != nil {
			continue
		}
		points = append(points, p)
	}
	return points, nil
}

// getRecentAlerts returns the most recent alert events
func getRecentAlerts(limit int) ([]RecentEvent, error) {
	rows, err := db.Query(
		`SELECT id, timestamp, type, message, probability, classification 
		 FROM events WHERE type='alert' ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []RecentEvent
	for rows.Next() {
		var e RecentEvent
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Type, &e.Message, &e.Probability, &e.Classification); err != nil {
			continue
		}
		events = append(events, e)
	}
	return events, nil
}
