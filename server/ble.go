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
	Device     BLEDevice `json:"device"`
	Connected  bool      `json:"connected"`
	Connecting bool      `json:"connecting,omitempty"`
	LastSeen   time.Time `json:"last_seen"`
	Error      string    `json:"error,omitempty"`

	conn    bluetooth.Device
	rxChar  bluetooth.DeviceCharacteristic // write to device (server -> bangle)
	buf     string                         // partial line buffer for incoming data
	writeMu sync.Mutex                     // keep complete REPL commands from interleaving
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
	alertsOn    bool            // whether to send alerts to devices
	stopReconn  chan struct{}   // signal to stop reconnection loop
	threshold   int
	modelScores bool
	hasSample   bool
	ambient     int
	crying      int
	babbling    int
	sampleAt    time.Time
	checkAt     time.Time
	lastUpdate  time.Time
	lastBeat    time.Time
	updateTimer *time.Timer
	telemetryID uint64
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
	// Reserve the connection attempt so the reconnect loop/API cannot start a
	// second installer whose REPL writes would interleave with this one.
	if cs, ok := m.connections[dev.Address]; ok {
		if cs.Connected || cs.Connecting {
			m.mu.Unlock()
			return
		}
		cs.Connecting = true
		cs.Error = ""
	} else {
		m.connections[dev.Address] = &BLEConnectionState{Device: dev, Connecting: true}
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

	m.mu.Lock()
	m.connections[dev.Address] = cs
	m.mu.Unlock()

	if err := txChar.EnableNotifications(func(buf []byte) {
		m.handleIncomingData(dev.Address, cs, string(buf))
	}); err != nil {
		conn.Disconnect()
		m.setConnectionError(dev, "UART notifications failed: "+err.Error())
		return
	}

	log.Printf("[ble] Connected to %s (%s)", dev.Name, dev.Address)

	// Send initial setup code to Bangle.js
	m.sendSetupCode(dev.Address)
}

// handleIncomingData processes data received from a BLE device.
// Data arrives in chunks; we buffer until we see a newline.
func (m *BLEManager) handleIncomingData(addr string, source *BLEConnectionState, data string) {
	m.mu.Lock()
	cs, ok := m.connections[addr]
	if !ok || cs != source {
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

		// Espruino may prefix Bluetooth.println output with a REPL prompt and
		// control characters. Extract the JSON object instead of requiring it at
		// byte zero, otherwise valid watch actions are only logged and ignored.
		if cmd, ok := parseBLECommand(line); ok {
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
		log.Printf("[ble] %s: %s", addr, line)
	}
	m.mu.Unlock()
}

func parseBLECommand(line string) (BLECommand, bool) {
	start := strings.IndexByte(line, '{')
	end := strings.LastIndexByte(line, '}')
	if start < 0 || end < start {
		return BLECommand{}, false
	}
	var cmd BLECommand
	if err := json.Unmarshal([]byte(line[start:end+1]), &cmd); err != nil || cmd.Type != "cmd" || cmd.Command == "" {
		return BLECommand{}, false
	}
	return cmd, true
}

// bangleAppJS is the Baby Monitor app for Bangle.js 1 and 2.
// It installs as a proper app visible in the launcher.
// The app shows monitoring status, allows start/stop, and handles incoming alerts.
// Compatible with Bangle.js 1 (2v08+) and Bangle.js 2.
const bangleAppJS = `var s={run:0,ready:0,alert:0,pending:0,model:1,th:80,val:-1,beat:0,sample:0,points:[],ver:-1};
var _bc=0,_cmd,_poll,_tick,_buzz,_bi=0,_actionAt=0,_pat=[[150,100],[150,100],[150,300],[400,150],[400,150],[400,300],[150,100],[150,100],[150,800]];
var _store=require("Storage"),_cryImg=_store.read("babymon.cry"),_lisImg=_store.read("babymon.lis");
try{_bc=!!NRF.getSecurityStatus().connected;}catch(e){}
var _btn=typeof BTN2!="undefined"?BTN2:(typeof BTN1!="undefined"?BTN1:undefined),_back=typeof BTN3!="undefined"?BTN3:undefined;
function send(c){if(_bc)Bluetooth.println(JSON.stringify({t:"cmd",cmd:c}));}
function buzz(){if(!s.alert)return;var p=_pat[_bi++%_pat.length];Bangle.buzz(p[0]);_buzz=setTimeout(buzz,p[0]+p[1]);}
function txt(x,y,v,z,c){g.setFont("Vector",z).setFontAlign(0,0).setColor(c);g.drawString(v,x,y);}
function draw(){var W=g.getWidth(),H=g.getHeight(),wide=W>200,now=Date.now();g.clear().reset();
txt(W/2,10,_bc?"Connected":"CONNECTION LOST",wide?15:13,_bc?"#78c77d":"#ff5c5c");
if(s.alert){if(_cryImg)g.drawImage(_cryImg,W/2-(wide?24:12),wide?24:21,{scale:wide?1:0.5});txt(W/2,wide?88:58,"Baby crying",wide?29:23,"#ff5555");txt(W/2,wide?122:83,"Check on your baby",wide?18:15,"#ffffff");txt(W/2,H-31,"Tap or press: Dismiss",wide?16:13,"#8fcbea");if(!_bc)txt(W/2,H-55,"Alerts cannot reach watch",13,"#ff7777");return;}
if(!_bc){txt(W/2,wide?66:53,"Connection lost",wide?28:23,"#ff5555");txt(W/2,wide?102:83,"Alerts cannot reach watch",wide?17:14,"#ffffff");txt(W/2,H-31,"Move near the monitor",wide?16:13,"#aaaaaa");return;}
var title,sub,act;if(s.pending){title=_cmd==="stop"?"Stopping monitor":"Starting monitor";sub=_cmd==="stop"?"Please wait":"Preparing microphone";act=_cmd==="start"?"Tap or press: Stop":"Please wait";}else if(!s.run){title="Monitor off";sub="No sounds are being checked";act="Tap or press: Start";}else{title="Listening";sub="You will be alerted to crying";act="Tap or press: Stop";}
if(s.run&&!s.pending&&_lisImg)g.drawImage(_lisImg,wide?14:9,wide?31:27,{scale:wide?0.75:0.5});txt(W/2,wide?43:35,title,wide?26:21,s.ready?"#78c77d":"#8fcbea");txt(W/2,wide?70:58,sub,wide?15:12,"#dddddd");var x=8,y=wide?91:76,w=W-16,h=wide?105:66;g.setColor("#333333").drawRect(x,y,x+w,y+h);
if(!s.model){txt(W/2,y+h/2-7,"Confidence unavailable",wide?17:14,"#bbbbbb");txt(W/2,y+h/2+13,"Loudness-only mode",wide?14:12,"#888888");}
else if(s.points.length){var ty=y+h-Math.round(s.th*h/100);g.setColor("#775555").drawLine(x,ty,x+w,ty);g.setFont("6x8",1).setFontAlign(-1,1).setColor("#bb7777").drawString(s.th+"%",x+2,ty-1);var n=s.points.length,dx=w/23;g.setColor("#ff7b8b");for(var i=1;i<n;i++)g.drawLine(x+w-(n-i)*dx,y+h-s.points[i-1]*h/100,x+w-(n-1-i)*dx,y+h-s.points[i]*h/100);for(i=0;i<n;i++)g.fillCircle(x+w-(n-1-i)*dx,y+h-s.points[i]*h/100,2);var recent=now-s.sample<16000,live=now-s.beat<16000;txt(W/2,y+11,(recent?"Cry confidence ":"Last cry score ")+s.val+"%",wide?17:14,recent?"#ffffff":"#bbbbbb");if(!recent)txt(W/2,y+h-9,live?"Quiet check - no new score":"No recent signal",wide?14:11,live?"#78c77d":"#ff7777");}
else{txt(W/2,y+h/2-7,"Waiting for signal",wide?17:14,"#bbbbbb");txt(W/2,y+h/2+13,now-s.beat<16000?"Quiet check - no score":"No recent signal",wide?14:11,"#888888");}txt(W/2,H-12,act,wide?16:13,"#8fcbea");}
function clearPending(){s.pending=0;_cmd=undefined;if(_poll)clearTimeout(_poll);_poll=undefined;}
function poll(){if(_poll)clearTimeout(_poll);if(!_bc)return;_poll=setTimeout(function(){send("status");if(s.pending)poll();},5000);}
function command(c,force){if(!_bc||s.pending&&!force)return;if(s.pending&&_cmd===c)return;s.pending=1;_cmd=c;send(c);poll();draw();}
function dismiss(stop){s.alert=0;if(_buzz)clearTimeout(_buzz);_buzz=undefined;send("dismiss");if(stop)command("stop",1);Bangle.buzz(100);draw();}
function action(){var n=Date.now();if(s.alert){_actionAt=n;dismiss(0);return;}if(n-_actionAt<650)return;_actionAt=n;if(s.pending){if(_cmd==="start")command("stop",1);return;}command(s.run?"stop":"start");Bangle.buzz(100);}
babymon_alert=function(){s.alert=1;_bi=0;buzz();draw();};
babymon_status=function(r,q,t,m,v){if(v!==undefined&&v<s.ver)return;var old=[s.run,s.ready,s.pending,_cmd,s.th,s.model].join();if(v!==undefined)s.ver=v;s.run=!!r;s.ready=!!q;s.th=t;s.model=!!m;if(_cmd==="stop"&&r){s.pending=1;poll();}else if(!r||q)clearPending();else{s.pending=1;_cmd="start";poll();}if(!s.alert&&old!==[s.run,s.ready,s.pending,_cmd,s.th,s.model].join())draw();};
babymon_data=function(a,c,b,age,add){s.val=c;s.sample=Date.now()-(age||0)*1000;s.beat=s.sample;if(add||!s.points.length){s.points.push(c);if(s.points.length>24)s.points.shift();}if(!s.alert)draw();};
babymon_beat=function(age){s.beat=Date.now()-(age||0)*1000;};
if(_btn!==undefined)setWatch(action,_btn,{repeat:true,edge:"rising"});if(_back!==undefined)setWatch(function(){if(s.alert)dismiss(1);else Bangle.showLauncher();},_back,{repeat:true,edge:"rising"});
function touch(){action();}function con(){_bc=1;draw();send("status");if(s.pending)poll();}function dis(){_bc=0;if(_poll)clearTimeout(_poll);_poll=undefined;draw();}
Bangle.on("touch",touch);NRF.on("connect",con);NRF.on("disconnect",dis);var _oldst=-1;_tick=setInterval(function(){var n=Date.now(),st=s.points.length?(n-s.sample>=16000?1:0)+(n-s.beat>=16000?2:0):n-s.beat>=16000?3:0;if(st!==_oldst){_oldst=st;if(!s.alert)draw();}},15000);
E.on("kill",function(){if(_poll)clearTimeout(_poll);if(_tick)clearInterval(_tick);if(_buzz)clearTimeout(_buzz);s.alert=0;Bangle.removeListener("touch",touch);NRF.removeListener("connect",con);NRF.removeListener("disconnect",dis);});draw();send("status");
`

// writeStorageFile writes a file to the Bangle.js Storage in chunks.
// Espruino's REPL has a limited input buffer, so we can't send the entire
// file in one command. Instead we use Storage.write(name, data, offset, totalSize)
// which allows appending to a file in pieces.
func (m *BLEManager) writeStorageFile(addr string, cs *BLEConnectionState, filename, content string) error {
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

		cmd := fmt.Sprintf("\x10require('Storage').write('%s',\"%s\",%d,%d);\n",
			filename, escaped, offset, total)

		if err := m.sendRaw(addr, cs, cmd); err != nil {
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
	m.mu.RLock()
	cs, ok := m.connections[addr]
	m.mu.RUnlock()
	if !ok || !cs.Connected {
		return
	}
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()

	// Erase old files
	m.sendRaw(addr, cs, "\x10require('Storage').erase('babymon.app.js');require('Storage').erase('babymon.cry');require('Storage').erase('babymon.ok');require('Storage').erase('babymon.lis');\n")
	time.Sleep(300 * time.Millisecond)

	// Write the crying baby image as a separate storage file
	cryImgCmd := "\x10require('Storage').write('babymon.cry',require('heatshrink').decompress(atob('nU5xH+64A/AH4A/AA/Q1er1nQG2WkvN4AAd50o4u0oyCN4XW1mkBAOsHFYvB0h2C1isD6x7BWVWrvCjB1itEVgd5vI5pvN6HAKuD6HPz14VYPQI4QAmFQPPUYN554AEvOk6wLCctIxBdAI5FAAedvPWG0fW6vPHImdHJJ/DHcI3BAAI5DAYPIHA/IP4o7d6wrFFQd5V4+keQ46bHAyeEboN4WIOk0jjBvA4GHbYhIVQJvENoI8BvI9BeBI6XEJSgHACA6U6ohLHVbjHHRClLABQ4fHQV4zo5U6rkS1lk1gdG0irDAgN4O4oNEAAmIxCvROQXQiwABHQ1OFYuk0nIAwlOXA1eEAI6DOSA5DnQpFpy4OJII7DxAgCOiDlEVoIZB6BwCHB46DOwZyBnQ4DOhoqPACCvGAAp0KOQgAJwF60oOL1YPOHLGlhAACmRHKB4dWHRisM1k6r2sCwssEoQ9BwAmIGoY9BOousxDqEHJk6iwABnQdEvRoET5IPF1YFDxAkCi3QVxYUCsgTCJwYAcTAIlCdBgVEVgwAd6GsOQQ5OAFY5R6FePDYcJHKGsdTodBsg5XfwRzbDwRYGHKbNMAAQPLxA5SEA2sxByI5OeuelDQlJvOkHRLLHHBA5HABGkp9IAAI5FBAVIvS3PHJKuHAA1zFwdIugYD0oKEpB3JVho5OpItFpFPvQABBQ1IzxyWVxg4HABp1LHK2eHClIpIhJ6CsWOSp0NdBDlMFZNtueXuY5VV5A5UufCAAmXHKh0HHJl5FAuXHAoACtoQFEhg5UOgo4JAAIQEzw5U6wVM0jiDHBSwEuZdNc450GII46C3YAB3g3G3gKBV4N5GI4iFHBB1C63QQAZ8Hzo5CABWX0ijJFAIpEACPVHY3INQQAG4XJbhwAWeZXJ5HI5I1HG8A7OABI3ieoo2N6o3mHo/WeYQzBAAI1rAH4AfA==')));\n"
	m.sendRaw(addr, cs, cryImgCmd)
	time.Sleep(500 * time.Millisecond)

	// Write the listening icon as a separate storage file
	lisImgCmd := "\x10require('Storage').write('babymon.lis',require('heatshrink').decompress(atob('lssxH+64A/AH4Aa63WE8vQz1stlzvV6uYFBvXQFT2lERXQ0ls0pVbDo3Q1msGQo7B1YrX0lzVwaGBuWd0mdQoNsvIwDvV5Fat5zwECudy5DdBAAXPtnP54LBFwWluQrUQQR2BFQgnBAYVyAYekCYOsFqWkK4WkuXVFQYiBzt5AwNyGIXWBAJBCRKHQV4WluaBEEofI5InBFgYFBvS3CewoAJNYXQPAQsELoaOBFgoGBFIVsFZusQgTSBFg4qBLoVsBooHBOQS6CLBuk0gdGKQQ2CzoOH56gCLRoQDDgxaCzxaCzoOJ6xIB1grK0urAQOeDpBMBMoKSGAAfIUQPQvJYNuYeKAByECQ5dsvV5QpIAQuVzzwsM0WiTIIAYzuc0QsJ0tytnS4zQJACGk0XSuT0BFYvQuWizgsG5+duV55LmIBgOdZAnIDwIhBzmlFgme4yEBFgvIDgIABMRAME0gsF42c5lyFgly5gsC5gjDDIYAO5CGF5l46VIbovMznGBoLgbDwYsILIXStgsZEAJZDtiGJd4LLEACfPtgdBWYJZGvXMQ4IOCWgVyWged5IjGvI+C5JwCWQXSJwPMLIulBANy6aHCDYJDBAAS8IzoNDHISFC6TVB5meFgiHBFAIOBHgJaB5+dFwPVFgXPuaUDAYN55OkNYOkOwQgBPgPQFgtz41yd4LiGXIly5HPL4gMELAaDBQowtDuRcB6XTQ4QeB5/QEgonD6ANBFgZHCzggBLAwADz1yB4IsD6ArDABANEYgN4vFzFJIADCgPXFgYADUwIAERQIHFToPXKpRdI5AdFztszrXBAgN5Boq9BFKIADLQ/P5IsBK4/PthWSFoueEIueLISSGFbAABDYOkKIfPLYJYE5FsvQqYAAeltlzQIvP5C2BzwqcAAfQ0tytgABpFy0iAZAH4A/AFg=')));\n"
	m.sendRaw(addr, cs, lisImgCmd)
	time.Sleep(500 * time.Millisecond)

	// Write the app code in chunks
	if err := m.writeStorageFile(addr, cs, "babymon.app.js", bangleAppJS); err != nil {
		log.Printf("[ble] Failed to write app to %s: %v", addr, err)
		return
	}
	time.Sleep(300 * time.Millisecond)

	// Write app info file (shows in launcher)
	infoCmd := "\x10require('Storage').write('babymon.info',JSON.stringify({id:'babymon',name:'Baby Monitor',src:'babymon.app.js',icon:'babymon.img'}));\n"
	m.sendRaw(addr, cs, infoCmd)
	time.Sleep(300 * time.Millisecond)

	// Write the app icon (heatshrink-compressed) for the launcher
	iconCmd := "\x10require('Storage').write('babymon.img',require('heatshrink').decompress(atob('lstxH+64A/AH4A/AGWrvVVq166Aqk616lUkAAVOwIrj0l40WizlOFoMq1Yqg6nN5gAD4wtCvPO6orc6oqFAAV4RAQFB6grbFQ4sH5nO6wrd414AgWicQQHDFoKvXKg1OlVOWITgB0QNEW63OFg2ilQqCAAIrF5nNRChYHRAZaBvHGBo7kUbpQAM5qFbACCHT5osXcSSyJFnjhSFjPOFn4sx0VOqnGEg9+BY3NFiIjFlUAkl4Fg4LBgALE43PFZ/PFgtOEAwLGvwsE44sP5wsF41UFYfGAAKSDBYgND6xZVAAosFBpBZQ6weLABqzRFoPNFi/OFaAtCDg94zhlC0V4NQ/VFaTjCDo2ip1PAATcFAAPNbp5aOKwRXILCwAB6qwmFq/OQiotF5orN6graW4QuK5pWbF5AAF6wqhAH4A/AH4A/AFYA==')));\n"
	m.sendRaw(addr, cs, iconCmd)
	time.Sleep(200 * time.Millisecond)

	// Launch the app
	m.sendRaw(addr, cs, "\x10load('babymon.app.js');\n")
	time.Sleep(500 * time.Millisecond)

	// Send initial status
	m.sendCurrentState(addr, cs)

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
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()
	return m.sendRaw(addr, cs, data)
}

func (m *BLEManager) sendRaw(addr string, cs *BLEConnectionState, data string) error {
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
			m.markDisconnected(addr, cs, "write error: "+err.Error())
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

// SendStatus sends the authoritative detector state to all connected watches.
func (m *BLEManager) SendStatus() {
	m.mu.RLock()
	addrs := make([]string, 0, len(m.connections))
	for addr, cs := range m.connections {
		if cs.Connected {
			addrs = append(addrs, addr)
		}
	}
	m.mu.RUnlock()

	for _, addr := range addrs {
		m.mu.RLock()
		cs := m.connections[addr]
		m.mu.RUnlock()
		if cs == nil {
			continue
		}
		cs.writeMu.Lock()
		m.sendCurrentState(addr, cs)
		cs.writeMu.Unlock()
	}
}

func (m *BLEManager) sendCurrentState(addr string, cs *BLEConnectionState) {
	running, ready, seq := detector.StateSnapshot()
	m.mu.RLock()
	threshold, model := m.threshold, m.modelScores
	hasSample, ambient, crying, babbling, sampleAt, checkAt := m.hasSample, m.ambient, m.crying, m.babbling, m.sampleAt, m.checkAt
	m.mu.RUnlock()
	cmd := fmt.Sprintf("\x10if(typeof babymon_status==='function')babymon_status(%t,%t,%d,%t,%d);\n", running, ready, threshold, model, seq)
	if hasSample && model {
		age := int(time.Since(sampleAt).Seconds())
		if age < 0 {
			age = 0
		}
		cmd += fmt.Sprintf("\x10if(typeof babymon_data==='function')babymon_data(%d,%d,%d,%d,0);\n", ambient, crying, babbling, age)
	}
	if !checkAt.IsZero() {
		age := int(time.Since(checkAt).Seconds())
		if age < 0 {
			age = 0
		}
		cmd += fmt.Sprintf("\x10if(typeof babymon_beat==='function')babymon_beat(%d);\n", age)
	}
	if err := m.sendRaw(addr, cs, cmd); err != nil {
		log.Printf("[ble] Failed to send status to %s: %v", addr, err)
	}
}

// ConfigureTelemetry resets run-specific samples and publishes the active threshold.
func (m *BLEManager) ConfigureTelemetry(cfg Config) {
	m.mu.Lock()
	m.threshold = int(cfg.ProbThreshold*100 + 0.5)
	m.modelScores = !cfg.ThresholdOnly
	m.hasSample = false
	m.sampleAt = time.Time{}
	m.checkAt = time.Time{}
	m.lastUpdate = time.Time{}
	m.lastBeat = time.Time{}
	m.telemetryID++
	if m.updateTimer != nil {
		m.updateTimer.Stop()
		m.updateTimer = nil
	}
	m.mu.Unlock()
}

const telemetryInterval = 12 * time.Second

// SendTelemetry retains every real result and coalesces watch graph updates.
func (m *BLEManager) SendTelemetry(ambient, crying, babbling int) {
	now := time.Now()
	m.mu.Lock()
	m.hasSample, m.ambient, m.crying, m.babbling, m.sampleAt, m.checkAt, m.lastBeat = true, ambient, crying, babbling, now, now, now
	remaining := telemetryInterval - now.Sub(m.lastUpdate)
	if !m.lastUpdate.IsZero() && remaining > 0 {
		if m.updateTimer == nil {
			id := m.telemetryID
			m.updateTimer = time.AfterFunc(remaining, func() { m.flushTelemetry(id) })
		}
		m.mu.Unlock()
		return
	}
	m.lastUpdate = now
	m.mu.Unlock()
	m.broadcast(fmt.Sprintf("\x10if(typeof babymon_data==='function')babymon_data(%d,%d,%d,0,1);\n", ambient, crying, babbling))
}

func (m *BLEManager) flushTelemetry(id uint64) {
	m.mu.Lock()
	if id != m.telemetryID || !m.hasSample {
		m.mu.Unlock()
		return
	}
	a, c, b := m.ambient, m.crying, m.babbling
	m.lastUpdate = time.Now()
	m.updateTimer = nil
	m.mu.Unlock()
	m.broadcast(fmt.Sprintf("\x10if(typeof babymon_data==='function')babymon_data(%d,%d,%d,0,1);\n", a, c, b))
}

// SendHeartbeat marks a real audio check without creating a confidence sample.
func (m *BLEManager) SendHeartbeat() {
	now := time.Now()
	m.mu.Lock()
	m.checkAt = now
	if now.Sub(m.lastBeat) < telemetryInterval {
		m.mu.Unlock()
		return
	}
	m.lastBeat = now
	m.mu.Unlock()
	m.broadcast("\x10if(typeof babymon_beat==='function')babymon_beat();\n")
}

func (m *BLEManager) broadcast(cmd string) {
	m.mu.RLock()
	addrs := make([]string, 0, len(m.connections))
	for addr, cs := range m.connections {
		if cs.Connected {
			addrs = append(addrs, addr)
		}
	}
	m.mu.RUnlock()
	for _, addr := range addrs {
		if err := m.SendRaw(addr, cmd); err != nil {
			log.Printf("[ble] Telemetry send failed to %s: %v", addr, err)
		}
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
			Device:     cs.Device,
			Connected:  cs.Connected,
			Connecting: cs.Connecting,
			LastSeen:   cs.LastSeen,
			Error:      cs.Error,
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

func (m *BLEManager) markDisconnected(addr string, source *BLEConnectionState, errMsg string) {
	m.mu.Lock()
	if cs, ok := m.connections[addr]; ok && cs == source {
		cs.Connected = false
		cs.Connecting = false
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
		case "start":
			if onStart != nil {
				onStart()
			}
		case "stop":
			if onStop != nil {
				onStop()
			}
		case "status":
		default:
			log.Printf("[ble] Unknown command: %s", cmd.Command)
			continue
		}
		m.SendStatus()
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
