package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// Nordic UART Service UUIDs used by Espruino/Bangle.js
var (
	nordicUARTServiceUUID = bluetooth.NewUUID([16]byte{0x6e, 0x40, 0x00, 0x01, 0xb5, 0xa3, 0xf3, 0x93, 0xe0, 0xa9, 0xe5, 0x0e, 0x24, 0xdc, 0xca, 0x9e})
	nordicUARTRXUUID      = bluetooth.NewUUID([16]byte{0x6e, 0x40, 0x00, 0x02, 0xb5, 0xa3, 0xf3, 0x93, 0xe0, 0xa9, 0xe5, 0x0e, 0x24, 0xdc, 0xca, 0x9e})
	nordicUARTTXUUID      = bluetooth.NewUUID([16]byte{0x6e, 0x40, 0x00, 0x03, 0xb5, 0xa3, 0xf3, 0x93, 0xe0, 0xa9, 0xe5, 0x0e, 0x24, 0xdc, 0xca, 0x9e})
)

// BLEConnectionState tracks the state of a single BLE device connection.
type BLEConnectionState struct {
	Device    BLEDevice `json:"device"`
	Connected bool      `json:"connected"`
	LastSeen  time.Time `json:"last_seen"`
	Error     string    `json:"error,omitempty"`

	conn   bluetooth.Device
	rxChar bluetooth.DeviceCharacteristic // write to device (server -> bangle)
	buf    string                         // partial line buffer for incoming data
}

// ScanResult is a BLE device found during scanning.
type ScanResult struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	RSSI    int    `json:"rssi"`
}

// BLECommand is a command received from a BLE device.
type BLECommand struct {
	Type    string `json:"t"`              // "cmd"
	Command string `json:"cmd"`            // "dismiss", "start", "stop"
	From    string `json:"from,omitempty"` // device address
}

// BLEManager manages all BLE device connections.
type BLEManager struct {
	mu          sync.RWMutex
	adapter     *bluetooth.Adapter
	enabled     bool
	connections map[string]*BLEConnectionState // keyed by MAC address
	scanning    bool
	scanResults []ScanResult
	scanMu      sync.Mutex
	cmdChan     chan BLECommand // channel for incoming commands from devices
	alertsOn    bool           // whether to send alerts to devices
	stopReconn  chan struct{}   // signal to stop reconnection loop
}

var bleManager = &BLEManager{
	connections: make(map[string]*BLEConnectionState),
	cmdChan:     make(chan BLECommand, 20),
}

// Init enables the BLE adapter. Call once at startup.
func (m *BLEManager) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.adapter = bluetooth.DefaultAdapter
	if err := m.adapter.Enable(); err != nil {
		log.Printf("[ble] Failed to enable BLE adapter: %v", err)
		return err
	}
	m.enabled = true
	log.Printf("[ble] BLE adapter enabled")
	return nil
}

// ConnectDevices connects to all configured BLE devices.
func (m *BLEManager) ConnectDevices(devices []BLEDevice, alertsOn bool) {
	m.mu.Lock()
	m.alertsOn = alertsOn
	m.mu.Unlock()

	for _, dev := range devices {
		go m.connectDevice(dev)
	}

	// Start reconnection loop
	m.mu.Lock()
	if m.stopReconn != nil {
		close(m.stopReconn)
	}
	m.stopReconn = make(chan struct{})
	m.mu.Unlock()
	go m.reconnectLoop()
}

func (m *BLEManager) connectDevice(dev BLEDevice) {
	m.mu.Lock()
	if !m.enabled {
		m.mu.Unlock()
		return
	}
	// Skip if already connected
	if cs, ok := m.connections[dev.Address]; ok && cs.Connected {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	log.Printf("[ble] Connecting to %s (%s)...", dev.Name, dev.Address)

	// Scan for the specific device
	var targetAddr bluetooth.Address
	found := make(chan bool, 1)

	err := m.adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		addr := result.Address.String()
		if strings.EqualFold(addr, dev.Address) {
			targetAddr = result.Address
			adapter.StopScan()
			found <- true
		}
	})
	if err != nil {
		m.setConnectionError(dev, "scan failed: "+err.Error())
		return
	}

	// Wait for scan result with timeout
	select {
	case <-found:
	case <-time.After(15 * time.Second):
		m.adapter.StopScan()
		m.setConnectionError(dev, "device not found during scan")
		return
	}

	// Connect to device
	conn, err := m.adapter.Connect(targetAddr, bluetooth.ConnectionParams{})
	if err != nil {
		m.setConnectionError(dev, "connect failed: "+err.Error())
		return
	}

	// Discover Nordic UART Service
	services, err := conn.DiscoverServices([]bluetooth.UUID{nordicUARTServiceUUID})
	if err != nil || len(services) == 0 {
		conn.Disconnect()
		m.setConnectionError(dev, "Nordic UART service not found")
		return
	}

	// Discover characteristics
	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{nordicUARTRXUUID, nordicUARTTXUUID})
	if err != nil || len(chars) < 2 {
		conn.Disconnect()
		m.setConnectionError(dev, "UART characteristics not found")
		return
	}

	var rxChar, txChar bluetooth.DeviceCharacteristic
	for _, c := range chars {
		switch {
		case c.UUID() == nordicUARTRXUUID:
			rxChar = c // we write to this (server -> bangle)
		case c.UUID() == nordicUARTTXUUID:
			txChar = c // we read from this (bangle -> server)
		}
	}

	// Set up notification handler for data coming FROM the Bangle
	cs := &BLEConnectionState{
		Device:    dev,
		Connected: true,
		LastSeen:  time.Now(),
		conn:      conn,
		rxChar:    rxChar,
	}

	txChar.EnableNotifications(func(buf []byte) {
		m.handleIncomingData(dev.Address, string(buf))
	})

	m.mu.Lock()
	m.connections[dev.Address] = cs
	m.mu.Unlock()

	log.Printf("[ble] Connected to %s (%s)", dev.Name, dev.Address)

	// Send initial setup code to Bangle.js
	m.sendSetupCode(dev.Address)
}

// handleIncomingData processes data received from a BLE device.
// Data arrives in chunks; we buffer until we see a newline.
func (m *BLEManager) handleIncomingData(addr, data string) {
	m.mu.Lock()
	cs, ok := m.connections[addr]
	if !ok {
		m.mu.Unlock()
		return
	}
	cs.buf += data
	cs.LastSeen = time.Now()

	// Process complete lines
	for {
		idx := strings.Index(cs.buf, "\n")
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(cs.buf[:idx])
		cs.buf = cs.buf[idx+1:]

		if line == "" {
			continue
		}

		// Try to parse as JSON command
		if strings.HasPrefix(line, "{") {
			var cmd BLECommand
			if err := json.Unmarshal([]byte(line), &cmd); err == nil && cmd.Type == "cmd" {
				cmd.From = addr
				m.mu.Unlock()
				select {
				case m.cmdChan <- cmd:
				default:
					log.Printf("[ble] Command channel full, dropping: %+v", cmd)
				}
				m.mu.Lock()
				cs = m.connections[addr]
				if cs == nil {
					m.mu.Unlock()
					return
				}
				continue
			}
		}
		log.Printf("[ble] %s: %s", addr, line)
	}
	m.mu.Unlock()
}

// bangleAppJS is the Baby Monitor app for Bangle.js 1 and 2.
// It installs as a proper app visible in the launcher.
// The app shows monitoring status, allows start/stop, and handles incoming alerts.
// Compatible with Bangle.js 1 (2v08+) and Bangle.js 2.
const bangleAppJS = `var s={run:false,alert:false,alrt:0,cry:0,chk:0};
var _s,_t,_p=[[150,100],[150,100],[150,300],[400,150],[400,150],[400,300],[150,100],[150,100],[150,800]];
function _bn(){if(!_s)return;var e=_p[_t%_p.length];Bangle.buzz(e[0]);_t++;setTimeout(_bn,e[0]+e[1]);}
var cryImg=require("Storage").read("babymon.cry");
var lisImg=require("Storage").read("babymon.lis");
function draw(){
g.clear();g.reset();
g.setFontAlign(0,0);
var W=g.getWidth(),H=g.getHeight();
if(s.alert){
g.setFont("Vector",22);g.setColor(1,0.3,0.3);
g.drawString("Baby Crying",W/2,22);
g.drawImage(cryImg,W/2-24,42);
g.setFont("6x8",2);g.setColor(1,1,1);
g.drawString("BTN2:Dismiss",W/2,H-50);
g.setFont("6x8",1);g.setColor(0.6,0.6,0.6);
g.drawString("BTN3:Stop monitor",W/2,H-28);
return;
}
if(s.run){
g.setFont("Vector",22);g.setColor(0.48,0.78,0.49);
g.drawString("Listening",W/2,22);
g.drawImage(lisImg,W/2-24,42);
g.setFont("6x8",2);g.setColor(1,1,1);
g.drawString("Alerts:"+s.alrt+" Cry:"+s.cry,W/2,100);
g.setFont("6x8",2);g.setColor(0.5,0.72,0.82);
g.drawString("BTN2:Stop",W/2,H-15);
}else{
g.setFont("Vector",22);g.setColor(0.72,0.66,0.83);
g.drawString("Stopped",W/2,35);
g.setFont("6x8",2);g.setColor(1,1,1);
g.drawString("Alerts: "+s.alrt,W/2,65);
g.drawString("Crying: "+s.cry,W/2,85);
g.drawString("Checks: "+s.chk,W/2,105);
g.setFont("6x8",2);g.setColor(0.5,0.72,0.82);
g.drawString("BTN2:Start",W/2,H-15);
}
}
babymon_alert=function(m){
s.alert=true;s.alrt++;
_s=1;_t=0;_bn();
draw();
};
babymon_status=function(r,a,c,k){
s.run=r;if(a!==undefined)s.alrt=a;if(c!==undefined)s.cry=c;if(k!==undefined)s.chk=k;
if(!s.alert)draw();
};
setWatch(function(){
if(s.alert){
s.alert=false;_s=0;
Bluetooth.println(JSON.stringify({t:"cmd",cmd:"dismiss"}));
Bangle.buzz(100);draw();
return;
}
if(s.run){
Bluetooth.println(JSON.stringify({t:"cmd",cmd:"stop"}));
s.run=false;
}else{
Bluetooth.println(JSON.stringify({t:"cmd",cmd:"start"}));
s.run=true;
}
Bangle.buzz(100);
draw();
},BTN2,{repeat:true,edge:"rising"});
setWatch(function(){
if(s.alert){
s.alert=false;_s=0;
Bluetooth.println(JSON.stringify({t:"cmd",cmd:"stop"}));
s.run=false;
Bangle.buzz(100);draw();
return;
}
Bangle.showLauncher();
},BTN3,{repeat:true,edge:"rising"});
draw();
`

// writeStorageFile writes a file to the Bangle.js Storage in chunks.
// Espruino's REPL has a limited input buffer, so we can't send the entire
// file in one command. Instead we use Storage.write(name, data, offset, totalSize)
// which allows appending to a file in pieces.
func (m *BLEManager) writeStorageFile(addr, filename, content string) error {
	total := len(content)
	// Chunk size for the data portion. The full command is:
	//   \x10require('Storage').write('name',"chunk",offset,total);\n
	// We keep chunks small enough to not overflow the REPL buffer (~512 bytes).
	const chunkSize = 256

	for offset := 0; offset < total; offset += chunkSize {
		end := offset + chunkSize
		if end > total {
			end = total
		}
		chunk := content[offset:end]

		// Escape the chunk for a JS double-quoted string
		escaped := strings.ReplaceAll(chunk, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		escaped = strings.ReplaceAll(escaped, "\n", "\\n")
		escaped = strings.ReplaceAll(escaped, "\r", "\\r")

		cmd := fmt.Sprintf("\x10require('Storage').write('babymon.app.js',\"%s\",%d,%d);\n",
			escaped, offset, total)

		if err := m.SendRaw(addr, cmd); err != nil {
			return fmt.Errorf("write chunk at offset %d: %w", offset, err)
		}
		time.Sleep(200 * time.Millisecond) // Give the watch time to process
	}
	return nil
}

// sendSetupCode installs the Baby Monitor app on the Bangle.js.
// It writes the app code and info file to the watch's storage,
// then launches the app.
func (m *BLEManager) sendSetupCode(addr string) {
	log.Printf("[ble] Installing Baby Monitor app on %s...", addr)

	// Erase old files
	m.SendRaw(addr, "\x10require('Storage').erase('babymon.app.js');require('Storage').erase('babymon.cry');require('Storage').erase('babymon.ok');require('Storage').erase('babymon.lis');\n")
	time.Sleep(300 * time.Millisecond)

	// Write the crying baby image as a separate storage file
	cryImgCmd := "\x10require('Storage').write('babymon.cry',require('heatshrink').decompress(atob('nU5xH+64A/AH4A/AA/Q1er1nQG2WkvN4AAd50o4u0oyCN4XW1mkBAOsHFYvB0h2C1isD6x7BWVWrvCjB1itEVgd5vI5pvN6HAKuD6HPz14VYPQI4QAmFQPPUYN554AEvOk6wLCctIxBdAI5FAAedvPWG0fW6vPHImdHJJ/DHcI3BAAI5DAYPIHA/IP4o7d6wrFFQd5V4+keQ46bHAyeEboN4WIOk0jjBvA4GHbYhIVQJvENoI8BvI9BeBI6XEJSgHACA6U6ohLHVbjHHRClLABQ4fHQV4zo5U6rkS1lk1gdG0irDAgN4O4oNEAAmIxCvROQXQiwABHQ1OFYuk0nIAwlOXA1eEAI6DOSA5DnQpFpy4OJII7DxAgCOiDlEVoIZB6BwCHB46DOwZyBnQ4DOhoqPACCvGAAp0KOQgAJwF60oOL1YPOHLGlhAACmRHKB4dWHRisM1k6r2sCwssEoQ9BwAmIGoY9BOousxDqEHJk6iwABnQdEvRoET5IPF1YFDxAkCi3QVxYUCsgTCJwYAcTAIlCdBgVEVgwAd6GsOQQ5OAFY5R6FePDYcJHKGsdTodBsg5XfwRzbDwRYGHKbNMAAQPLxA5SEA2sxByI5OeuelDQlJvOkHRLLHHBA5HABGkp9IAAI5FBAVIvS3PHJKuHAA1zFwdIugYD0oKEpB3JVho5OpItFpFPvQABBQ1IzxyWVxg4HABp1LHK2eHClIpIhJ6CsWOSp0NdBDlMFZNtueXuY5VV5A5UufCAAmXHKh0HHJl5FAuXHAoACtoQFEhg5UOgo4JAAIQEzw5U6wVM0jiDHBSwEuZdNc450GII46C3YAB3g3G3gKBV4N5GI4iFHBB1C63QQAZ8Hzo5CABWX0ijJFAIpEACPVHY3INQQAG4XJbhwAWeZXJ5HI5I1HG8A7OABI3ieoo2N6o3mHo/WeYQzBAAI1rAH4AfA==')));\n"
	m.SendRaw(addr, cryImgCmd)
	time.Sleep(500 * time.Millisecond)

	// Write the listening icon as a separate storage file
	lisImgCmd := "\x10require('Storage').write('babymon.lis',require('heatshrink').decompress(atob('lssxH+64A/AH4Aa63WE8vQz1stlzvV6uYFBvXQFT2lERXQ0ls0pVbDo3Q1msGQo7B1YrX0lzVwaGBuWd0mdQoNsvIwDvV5Fat5zwECudy5DdBAAXPtnP54LBFwWluQrUQQR2BFQgnBAYVyAYekCYOsFqWkK4WkuXVFQYiBzt5AwNyGIXWBAJBCRKHQV4WluaBEEofI5InBFgYFBvS3CewoAJNYXQPAQsELoaOBFgoGBFIVsFZusQgTSBFg4qBLoVsBooHBOQS6CLBuk0gdGKQQ2CzoOH56gCLRoQDDgxaCzxaCzoOJ6xIB1grK0urAQOeDpBMBMoKSGAAfIUQPQvJYNuYeKAByECQ5dsvV5QpIAQuVzzwsM0WiTIIAYzuc0QsJ0tytnS4zQJACGk0XSuT0BFYvQuWizgsG5+duV55LmIBgOdZAnIDwIhBzmlFgme4yEBFgvIDgIABMRAME0gsF42c5lyFgly5gsC5gjDDIYAO5CGF5l46VIbovMznGBoLgbDwYsILIXStgsZEAJZDtiGJd4LLEACfPtgdBWYJZGvXMQ4IOCWgVyWged5IjGvI+C5JwCWQXSJwPMLIulBANy6aHCDYJDBAAS8IzoNDHISFC6TVB5meFgiHBFAIOBHgJaB5+dFwPVFgXPuaUDAYN55OkNYOkOwQgBPgPQFgtz41yd4LiGXIly5HPL4gMELAaDBQowtDuRcB6XTQ4QeB5/QEgonD6ANBFgZHCzggBLAwADz1yB4IsD6ArDABANEYgN4vFzFJIADCgPXFgYADUwIAERQIHFToPXKpRdI5AdFztszrXBAgN5Boq9BFKIADLQ/P5IsBK4/PthWSFoueEIueLISSGFbAABDYOkKIfPLYJYE5FsvQqYAAeltlzQIvP5C2BzwqcAAfQ0tytgABpFy0iAZAH4A/AFg=')));\n"
	m.SendRaw(addr, lisImgCmd)
	time.Sleep(500 * time.Millisecond)

	// Write the app code in chunks
	if err := m.writeStorageFile(addr, "babymon.app.js", bangleAppJS); err != nil {
		log.Printf("[ble] Failed to write app to %s: %v", addr, err)
		return
	}
	time.Sleep(300 * time.Millisecond)

	// Write app info file (shows in launcher)
	infoCmd := "\x10require('Storage').write('babymon.info',JSON.stringify({id:'babymon',name:'Baby Monitor',src:'babymon.app.js',icon:'babymon.img'}));\n"
	m.SendRaw(addr, infoCmd)
	time.Sleep(300 * time.Millisecond)

	// Write the app icon (heatshrink-compressed) for the launcher
	iconCmd := "\x10require('Storage').write('babymon.img',require('heatshrink').decompress(atob('lstxH+64A/AH4A/AGWrvVVq166Aqk616lUkAAVOwIrj0l40WizlOFoMq1Yqg6nN5gAD4wtCvPO6orc6oqFAAV4RAQFB6grbFQ4sH5nO6wrd414AgWicQQHDFoKvXKg1OlVOWITgB0QNEW63OFg2ilQqCAAIrF5nNRChYHRAZaBvHGBo7kUbpQAM5qFbACCHT5osXcSSyJFnjhSFjPOFn4sx0VOqnGEg9+BY3NFiIjFlUAkl4Fg4LBgALE43PFZ/PFgtOEAwLGvwsE44sP5wsF41UFYfGAAKSDBYgND6xZVAAosFBpBZQ6weLABqzRFoPNFi/OFaAtCDg94zhlC0V4NQ/VFaTjCDo2ip1PAATcFAAPNbp5aOKwRXILCwAB6qwmFq/OQiotF5orN6graW4QuK5pWbF5AAF6wqhAH4A/AH4A/AFYA==')));\n"
	m.SendRaw(addr, iconCmd)
	time.Sleep(200 * time.Millisecond)

	// Launch the app
	m.SendRaw(addr, "\x10load('babymon.app.js');\n")
	time.Sleep(500 * time.Millisecond)

	// Send initial status
	running := detector.IsRunning()
	if running {
		m.SendRaw(addr, "\x10babymon_status(true);\n")
	} else {
		m.SendRaw(addr, "\x10babymon_status(false);\n")
	}

	log.Printf("[ble] App installed on %s", addr)
}

// alertJS sends a command to trigger the alert in the Baby Monitor app on the watch.
// If the app is loaded, it calls babymon_alert(). If not, it falls back to
// a self-contained inline alert with SOS vibration.
func alertJS(msg string) string {
	safeMsg := strings.ReplaceAll(msg, "\\", "\\\\")
	safeMsg = strings.ReplaceAll(safeMsg, "'", "\\'")
	safeMsg = strings.ReplaceAll(safeMsg, "\n", "\\n")
	// Try the app function first; fall back to inline if app not loaded
	return "\x10" +
		"if(typeof babymon_alert==='function'){babymon_alert('" + safeMsg + "');}else{" +
		"var _s=0,_t;" +
		"var _p=[[150,100],[150,100],[150,300],[400,150],[400,150],[400,300],[150,100],[150,100],[150,800]];" +
		"function _n(){if(!_s)return;var e=_p[_t%_p.length];Bangle.buzz(e[0]);_t++;setTimeout(_n,e[0]+e[1]);}" +
		"_s=1;_t=0;_n();" +
		"E.showPrompt('" + safeMsg + "',{title:'Baby Monitor',buttons:{Dismiss:'dismiss',Stop:'stop'}}).then(function(v){" +
		"_s=0;Bluetooth.println(JSON.stringify({t:'cmd',cmd:v}));Bangle.buzz(100);load();});}\n"
}


// SendRaw sends raw text to a device via Nordic UART RX characteristic.
// BLE MTU is typically 20 bytes, so we chunk the data.
func (m *BLEManager) SendRaw(addr string, data string) error {
	m.mu.RLock()
	cs, ok := m.connections[addr]
	if !ok || !cs.Connected {
		m.mu.RUnlock()
		return fmt.Errorf("device %s not connected", addr)
	}
	m.mu.RUnlock()

	// Send in chunks (BLE MTU is typically 20 bytes for NUS)
	const chunkSize = 20
	bytes := []byte(data)
	for i := 0; i < len(bytes); i += chunkSize {
		end := i + chunkSize
		if end > len(bytes) {
			end = len(bytes)
		}
		chunk := bytes[i:end]
		if _, err := cs.rxChar.WriteWithoutResponse(chunk); err != nil {
			log.Printf("[ble] Write error to %s: %v", addr, err)
			m.markDisconnected(addr, "write error: "+err.Error())
			return err
		}
		// Small delay between chunks to avoid overwhelming the device
		if end < len(bytes) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil
}

// SendAlert sends a cry alert to all connected BLE devices.
func (m *BLEManager) SendAlert(message string) {
	m.mu.RLock()
	if !m.alertsOn {
		m.mu.RUnlock()
		return
	}
	addrs := make([]string, 0, len(m.connections))
	for addr, cs := range m.connections {
		if cs.Connected {
			addrs = append(addrs, addr)
		}
	}
	m.mu.RUnlock()

	if len(addrs) == 0 {
		return
	}

	cmd := alertJS(message)
	for _, addr := range addrs {
		if err := m.SendRaw(addr, cmd); err != nil {
			log.Printf("[ble] Failed to send alert to %s: %v", addr, err)
		}
	}
	log.Printf("[ble] Alert sent to %d device(s)", len(addrs))
}

// SendStatus sends a status update to all connected BLE devices.
// It sends detector running state to update the watch app UI.
func (m *BLEManager) SendStatus(status string) {
	m.mu.RLock()
	addrs := make([]string, 0, len(m.connections))
	for addr, cs := range m.connections {
		if cs.Connected {
			addrs = append(addrs, addr)
		}
	}
	m.mu.RUnlock()

	running := detector.IsRunning()
	runStr := "false"
	if running {
		runStr = "true"
	}
	cmd := fmt.Sprintf("\x10if(typeof babymon_status==='function'){babymon_status(%s);}else{g.clear();g.setFont('6x8',2);g.setFontAlign(0,0);g.drawString('%s',g.getWidth()/2,g.getHeight()/2);g.flip();setTimeout(load,3000);}\n",
		runStr, strings.ReplaceAll(status, "'", "\\'"))

	for _, addr := range addrs {
		m.SendRaw(addr, cmd)
	}
}

// Scan discovers nearby BLE devices (filtered to Bangle.js / Espruino).
func (m *BLEManager) Scan(duration time.Duration) ([]ScanResult, error) {
	m.scanMu.Lock()
	if m.scanning {
		m.scanMu.Unlock()
		return nil, fmt.Errorf("scan already in progress")
	}
	m.scanning = true
	m.scanResults = nil
	m.scanMu.Unlock()

	defer func() {
		m.scanMu.Lock()
		m.scanning = false
		m.scanMu.Unlock()
	}()

	if !m.enabled {
		if err := m.Init(); err != nil {
			return nil, err
		}
	}

	seen := make(map[string]bool)
	done := make(chan struct{})

	go func() {
		time.Sleep(duration)
		m.adapter.StopScan()
		close(done)
	}()

	err := m.adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		addr := result.Address.String()
		name := result.LocalName()

		// Only include Bangle.js and Espruino devices
		if !isBangleDevice(name) {
			return
		}

		if seen[addr] {
			return
		}
		seen[addr] = true

		m.scanMu.Lock()
		m.scanResults = append(m.scanResults, ScanResult{
			Address: addr,
			Name:    name,
			RSSI:    int(result.RSSI),
		})
		m.scanMu.Unlock()
	})

	<-done

	if err != nil {
		return nil, err
	}

	m.scanMu.Lock()
	results := make([]ScanResult, len(m.scanResults))
	copy(results, m.scanResults)
	m.scanMu.Unlock()

	return results, nil
}

// GetStatus returns the connection status of all configured devices.
func (m *BLEManager) GetStatus() []BLEConnectionState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var states []BLEConnectionState
	for _, cs := range m.connections {
		states = append(states, BLEConnectionState{
			Device:    cs.Device,
			Connected: cs.Connected,
			LastSeen:  cs.LastSeen,
			Error:     cs.Error,
		})
	}
	return states
}

// DisconnectDevice disconnects a specific device.
func (m *BLEManager) DisconnectDevice(addr string) {
	m.mu.Lock()
	cs, ok := m.connections[addr]
	if ok && cs.Connected {
		cs.conn.Disconnect()
		cs.Connected = false
	}
	delete(m.connections, addr)
	m.mu.Unlock()
	log.Printf("[ble] Disconnected %s", addr)
}

// DisconnectAll disconnects all devices and stops the reconnection loop.
func (m *BLEManager) DisconnectAll() {
	m.mu.Lock()
	if m.stopReconn != nil {
		close(m.stopReconn)
		m.stopReconn = nil
	}
	for addr, cs := range m.connections {
		if cs.Connected {
			cs.conn.Disconnect()
		}
		delete(m.connections, addr)
	}
	m.mu.Unlock()
}

// reconnectLoop periodically tries to reconnect to disconnected devices.
func (m *BLEManager) reconnectLoop() {
	m.mu.RLock()
	stop := m.stopReconn
	m.mu.RUnlock()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			m.mu.RLock()
			var toReconnect []BLEDevice
			for _, cs := range m.connections {
				if !cs.Connected {
					toReconnect = append(toReconnect, cs.Device)
				}
			}
			m.mu.RUnlock()

			for _, dev := range toReconnect {
				log.Printf("[ble] Attempting reconnect to %s (%s)", dev.Name, dev.Address)
				m.connectDevice(dev)
			}
		}
	}
}

func (m *BLEManager) setConnectionError(dev BLEDevice, errMsg string) {
	m.mu.Lock()
	m.connections[dev.Address] = &BLEConnectionState{
		Device:    dev,
		Connected: false,
		LastSeen:  time.Now(),
		Error:     errMsg,
	}
	m.mu.Unlock()
	log.Printf("[ble] %s (%s): %s", dev.Name, dev.Address, errMsg)
}

func (m *BLEManager) markDisconnected(addr, errMsg string) {
	m.mu.Lock()
	if cs, ok := m.connections[addr]; ok {
		cs.Connected = false
		cs.Error = errMsg
	}
	m.mu.Unlock()
}

// ProcessCommands runs a loop that reads BLE commands and acts on them.
// Call in a goroutine. Uses the provided callbacks for start/stop/dismiss.
func (m *BLEManager) ProcessCommands(onDismiss func(), onStart func(), onStop func()) {
	for cmd := range m.cmdChan {
		log.Printf("[ble] Command from %s: %s", cmd.From, cmd.Command)
		switch cmd.Command {
		case "dismiss":
			if onDismiss != nil {
				onDismiss()
			}
			m.SendStatus("Alert dismissed")
		case "start":
			if onStart != nil {
				onStart()
			}
			m.SendStatus("Monitoring...")
		case "stop":
			if onStop != nil {
				onStop()
			}
			m.SendStatus("Stopped")
		default:
			log.Printf("[ble] Unknown command: %s", cmd.Command)
		}
	}
}

// isBangleDevice checks if a BLE device name looks like a Bangle.js or Espruino.
func isBangleDevice(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "bangle") ||
		strings.Contains(lower, "espruino") ||
		strings.Contains(lower, "puck.js") ||
		strings.Contains(lower, "pixl.js")
}

// classifyBangleDevice guesses the device type from its name.
func classifyBangleDevice(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "bangle.js2") || strings.Contains(lower, "banglejs2"):
		return "banglejs2"
	case strings.Contains(lower, "bangle"):
		return "banglejs1"
	default:
		return "generic"
	}
}
