package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ReefBlastDaemon struct {
	mu           sync.Mutex
	writeMu      sync.Mutex
	ws           *websocket.Conn
	defs         *GameDefs
	defsLock     sync.RWMutex
	defsPath     string
	policyPath   string
	statusPath   string
	logPath      string
	botName      string
	botPass      string
	token        string
	connected    bool
	kicked       bool
	kickWaitTill time.Time
	stopped      bool
	stopChan     chan struct{}

	// Policy
	policy Policy

	// Navigation & Harbor physics
	pos            Position
	vx             float64
	vz             float64
	isSailing      bool
	isEnteringZone bool
	enteringZoneAt time.Time
	zoneEnteredAt  time.Time
	harborTicks    int
	lastHarborTrip time.Time
	managerBusy    bool

	// Game state
	me             *PlayerMe
	zone           *int
	mode           string
	slot           int
	slotX          float64
	hp             float64
	combo          int
	comboT         float64
	lastComboTime  time.Time
	fishes         map[int64]*FishState
	shots          map[int64]*ShotState
	lastAimSent    struct {
		x float64
		z float64
		f bool
		t int64
	}
	lastDeathTime  time.Time
	zoneDeaths     map[int]int
	lastMarketPoll time.Time
	marketSniped   bool
	needsRespec      bool
	hasAutoRespecced bool
	needsIslandRun   bool
	lastDefsCheck  time.Time

	// Party Carry State
	inParty              bool
	partyLeader          string
	partyWaitingInHarbor bool
	partyJoinedAt        time.Time
	lastPartyActivity    time.Time

	// Performance stats
	stats Stats

	// Zero-allocation snapshot parsing & generation stamping
	snapshotGen uint64
	scanner     *SnapshotScanner

	// Organic Human Simulation
	persona        int     // 0: Grinder (65%), 1: Trader (20%), 2: Explorer (15%)
	personaSeed    float64 // Deterministic seed derived from botName
	smoothAimX     float64 // Exponentially smoothed aiming coordinates
	smoothAimZ     float64
	aimInitialized bool
}

func NewReefBlastDaemon(defs *GameDefs, botName, botPass, token string) *ReefBlastDaemon {
	baseDir := "."
	logsDir := filepath.Join(baseDir, "logs")
	_ = os.MkdirAll(logsDir, 0755)
	return NewReefBlastDaemonWithPaths(defs, botName, botPass, token, baseDir, filepath.Join(logsDir, "status.json"), filepath.Join(logsDir, "auto_progression.log"))
}

func NewReefBlastDaemonWithPaths(defs *GameDefs, botName, botPass, token, baseDir, statusPath, logPath string) *ReefBlastDaemon {
	var pSeed float64
	for _, c := range botName {
		pSeed = pSeed*31.37 + float64(c)
	}
	pHash := int(math.Abs(pSeed)) % 100
	persona := 0 // 0: Grinder (65%)
	if pHash >= 65 && pHash < 85 {
		persona = 1 // 1: Trader (20%)
	} else if pHash >= 85 {
		persona = 2 // 2: Explorer (15%)
	}

	d := &ReefBlastDaemon{
		defs:           defs,
		botName:        botName,
		botPass:        botPass,
		token:          token,
		policyPath:     filepath.Join(baseDir, "policy.json"),
		statusPath:     statusPath,
		logPath:        logPath,
		stopChan:       make(chan struct{}),
		pos:            Position{X: 0, Z: 10, R: 0},
		slot:           -1,
		slotX:          0,
		hp:             1000.0,
		fishes:         make(map[int64]*FishState),
		shots:          make(map[int64]*ShotState),
		zoneDeaths:     make(map[int]int),
		needsRespec:      false,
		hasAutoRespecced: false,
		needsIslandRun:   false,
		lastHarborTrip: time.Now(),
		zoneEnteredAt:  time.Now(),
		lastDefsCheck:  time.Now(),
		lastMarketPoll: time.Now().Add(-time.Duration(int(math.Abs(pSeed))%600) * time.Second),
		scanner:        NewSnapshotScanner(),
		persona:        persona,
		personaSeed:    pSeed,
		stats: Stats{
			StartedAt: time.Now().UnixMilli(),
		},
	}
	d.readPolicy()
	return d
}

func (d *ReefBlastDaemon) Log(category, format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02T15:04:05.000Z")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, category, msg)
	fmt.Print(line)

	if fi, err := os.Stat(d.logPath); err == nil && fi.Size() > 10*1024*1024 {
		_ = os.Rename(d.logPath, d.logPath+".1")
	}

	f, err := os.OpenFile(d.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		_, _ = f.WriteString(line)
	}
}

func (d *ReefBlastDaemon) readPolicy() {
	newPolicy := Policy{
		AllowBuying:        true,
		AllowSalvaging:     false,
		AllowMarketSelling: false,
		UpdatedAt:          time.Now(),
	}
	data, err := os.ReadFile(d.policyPath)
	if err == nil {
		var p Policy
		if json.Unmarshal(data, &p) == nil {
			newPolicy = p
		}
	}
	if envTarget := os.Getenv("RB_TARGET_WEAPON"); envTarget != "" && newPolicy.TargetWeapon == "" {
		newPolicy.TargetWeapon = envTarget
	}

	d.mu.Lock()
	// If target weapon is already owned in inventory, clear it so this exception will not repeat
	if newPolicy.TargetWeapon != "" && d.me != nil {
		for _, it := range d.me.Items {
			if it.W == newPolicy.TargetWeapon {
				newPolicy.TargetWeapon = ""
				break
			}
		}
	}
	d.policy = newPolicy
	d.mu.Unlock()
}

func (d *ReefBlastDaemon) savePolicyLocked() {
	if d.policyPath == "" {
		return
	}
	data, err := json.MarshalIndent(d.policy, "", "  ")
	if err == nil {
		_ = os.WriteFile(d.policyPath, data, 0644)
	}
}

func FetchGameDefs() (*GameDefs, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://greentea.id/reefblast/api/defs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var defs GameDefs
	if err := json.Unmarshal(body, &defs); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(body, &defs.Raw)
	return &defs, nil
}

func ApiLogin(name, pass string) (string, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	payload, _ := json.Marshal(map[string]string{"Name": name, "Pass": pass})
	resp, err := client.Post("https://greentea.id/reefblast/api/login", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}
	if res.Token == "" {
		if res.Error != "" {
			return "", fmt.Errorf("login failed: %s", res.Error)
		}
		return "", fmt.Errorf("login failed: missing token")
	}
	return res.Token, nil
}

func (d *ReefBlastDaemon) Start() {
	d.Log("SYSTEM", "=== Reefblast Autonomous Go Naval Daemon Starting (%s) ===", d.botName)
	d.Log("SYSTEM", "Policy: Buying=%v | Salvaging=%v | MarketSelling=%v", d.policy.AllowBuying, d.policy.AllowSalvaging, d.policy.AllowMarketSelling)

	if d.token == "" {
		d.Log("SYSTEM", "Authenticating via API as %s...", d.botName)
		tok, err := ApiLogin(d.botName, d.botPass)
		if err != nil {
			d.Log("ERROR", "Authentication failed for %s: %v. Will retry in supervisor...", d.botName, err)
		} else {
			d.token = tok
			d.Log("SYSTEM", "Authenticated successfully!")
		}
	}

	go d.connectionSupervisor()
	go d.managerLoop()
	go d.statusDumperLoop()
}

func (d *ReefBlastDaemon) Stop() {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	d.connected = false
	close(d.stopChan)
	d.writeMu.Lock()
	if d.ws != nil {
		_ = d.ws.Close()
	}
	d.writeMu.Unlock()
	d.mu.Unlock()
	d.Log("SYSTEM", "Daemon for '%s' stopped cleanly.", d.botName)
}

func (d *ReefBlastDaemon) connectionSupervisor() {
	for {
		select {
		case <-d.stopChan:
			return
		default:
		}

		if d.kicked && time.Now().Before(d.kickWaitTill) {
			rem := time.Until(d.kickWaitTill).Seconds()
			d.Log("SYSTEM", "Session kicked by another login. Waiting %.0fs before reconnecting...", rem)
			select {
			case <-d.stopChan:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		d.kicked = false

		if !d.connected {
			d.connectWS()
		}

		select {
		case <-d.stopChan:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (d *ReefBlastDaemon) connectWS() {
	if d.token == "" {
		tok, err := ApiLogin(d.botName, d.botPass)
		if err != nil {
			d.Log("ERROR", "Authentication retry failed for %s: %v", d.botName, err)
			return
		}
		d.token = tok
		d.Log("SYSTEM", "Authenticated successfully!")
	}

	wsURL := fmt.Sprintf("wss://greentea.id/reefblast/ws?token=%s", d.token)
	d.Log("SYSTEM", "Connecting to WebSocket wss://greentea.id/reefblast/ws?token=TOKEN_HIDDEN ...")

	header := http.Header{}
	header.Set("Origin", "https://greentea.id")
	header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		d.Log("ERROR", "WebSocket connection failed: %v", err)
		return
	}

	d.writeMu.Lock()
	d.ws = conn
	d.connected = true
	d.writeMu.Unlock()

	d.Log("SYSTEM", "WebSocket connection established!")
	go d.pingLoop(conn)
	d.readLoop(conn)
}

func (d *ReefBlastDaemon) Send(v interface{}) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.ws == nil || !d.connected {
		return fmt.Errorf("not connected")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return d.ws.WriteMessage(websocket.TextMessage, data)
}

// sendAimRaw serializes and transmits the aim packet with zero heap allocations
func (d *ReefBlastDaemon) sendAimRaw(x, z float64, firing bool) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.ws == nil || !d.connected {
		return fmt.Errorf("not connected")
	}

	var buf [64]byte
	b := buf[:0]
	b = append(b, `{"t":"aim","x":`...)
	b = strconv.AppendFloat(b, math.Round(x*100)/100, 'f', -1, 64)
	b = append(b, `,"z":`...)
	b = strconv.AppendFloat(b, math.Round(z*100)/100, 'f', -1, 64)
	b = append(b, `,"f":`...)
	if firing {
		b = append(b, "true}"...)
	} else {
		b = append(b, "false}"...)
	}

	return d.ws.WriteMessage(websocket.TextMessage, b)
}

// sendPingRaw serializes and transmits the ping packet with zero heap allocations
func (d *ReefBlastDaemon) sendPingRaw() error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.ws == nil || !d.connected {
		return fmt.Errorf("not connected")
	}

	var buf [48]byte
	b := buf[:0]
	b = append(b, `{"t":"ping","c":`...)
	b = strconv.AppendInt(b, time.Now().UnixMilli(), 10)
	b = append(b, '}')

	return d.ws.WriteMessage(websocket.TextMessage, b)
}

// sendMvRaw serializes and transmits movement packet with zero heap allocations
func (d *ReefBlastDaemon) sendMvRaw(x, z, r float64) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.ws == nil || !d.connected {
		return fmt.Errorf("not connected")
	}

	var buf [64]byte
	b := buf[:0]
	b = append(b, `{"t":"mv","x":`...)
	b = strconv.AppendFloat(b, math.Round(x*100)/100, 'f', -1, 64)
	b = append(b, `,"z":`...)
	b = strconv.AppendFloat(b, math.Round(z*100)/100, 'f', -1, 64)
	b = append(b, `,"r":`...)
	b = strconv.AppendFloat(b, math.Round(r*100)/100, 'f', -1, 64)
	b = append(b, '}')

	return d.ws.WriteMessage(websocket.TextMessage, b)
}

func (d *ReefBlastDaemon) pingLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		d.mu.Lock()
		active := d.connected && d.ws == conn
		d.mu.Unlock()
		if !active {
			return
		}
		_ = d.sendPingRaw()
	}
}

func (d *ReefBlastDaemon) readLoop(conn *websocket.Conn) {
	defer func() {
		d.mu.Lock()
		if d.ws == conn {
			d.connected = false
			d.zone = nil
			d.slot = -1
			d.isSailing = false
		}
		d.mu.Unlock()
		_ = conn.Close()
		d.Log("SYSTEM", "WebSocket connection closed.")
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			d.Log("SYSTEM", "Read error: %v", err)
			return
		}

		// Fast zero-allocation path for snapshots (30/sec)
		if len(message) > 6 && message[0] == '{' {
			prefixLen := min(40, len(message))
			if bytes.Contains(message[:prefixLen], []byte(`"t":"s"`)) || bytes.Contains(message[:prefixLen], []byte(`"t": "s"`)) {
				if d.handleSnapshotFast(message) {
					continue
				}
			}
		}

		var in IncomingMessage
		if err := json.Unmarshal(message, &in); err != nil {
			var env struct {
				T string `json:"t"`
			}
			_ = json.Unmarshal(message, &env)
			if env.T != "s" {
				d.Log("WARN", "WS unmarshal error for '%s': %v | raw: %s", env.T, err, string(message))
			}
			continue
		}
		d.handleIncoming(&in)
	}
}

func (d *ReefBlastDaemon) handleIncoming(m *IncomingMessage) {
	switch m.T {
	case "hello":
		d.mu.Lock()
		d.me = m.Me
		d.zone = nil
		d.mode = ""
		d.slot = -1
		d.slotX = 0
		if len(m.Pos) >= 2 {
			d.pos.X = m.Pos[0]
			d.pos.Z = m.Pos[1]
		} else {
			d.pos.X = 0
			d.pos.Z = 10
		}
		d.pos.R = 0
		d.vx = 0
		d.vz = 0
		d.isEnteringZone = false
		if d.stats.StartCoins == 0 && m.Me != nil {
			d.stats.StartCoins = m.Me.Coins
			d.stats.StartLevel = m.Me.Level
			d.stats.StartXp = m.Me.XP
		}
		me := d.me
		d.mu.Unlock()

		if me != nil {
			d.Log("SYSTEM", "Captain %s ready! Level: %d | Coins: %d | Vessel: %s", me.Name, me.Level, me.Coins, me.Boat)
			d.autoEquipBestWeapon()
			d.autoEquipBestBoat()
			d.autoEquipBestHelper()
			d.LogWeaponArsenal()
			d.DumpStatus()
		}

	case "room":
		d.mu.Lock()
		for _, p := range m.Players {
			if p.Slot == d.slot {
				d.slotX = p.X
				break
			}
		}
		d.mu.Unlock()

	case "me":
		d.mu.Lock()
		prev := d.me
		d.me = m.Me
		if prev != nil && m.Me != nil {
			if m.Me.Level > prev.Level {
				diff := m.Me.Level - prev.Level
				d.Log("MILESTONE", "Level Up! Captain %s is now Level %d! (+%d skill points) Coins: %d", m.Me.Name, m.Me.Level, diff, m.Me.Coins)
			}
			if len(m.Me.Items) > len(prev.Items) {
				prevUIDs := make(map[string]bool)
				for _, it := range prev.Items {
					prevUIDs[it.UID] = true
				}
				for i := range m.Me.Items {
					it := &m.Me.Items[i]
					if !prevUIDs[it.UID] {
						q := d.CalcItemQuality(it)
						it.Quality = q
						d.Log("ITEM", "Acquired weapon: %s (Quality: %d%%) [Atk: +%.1f%%, Aspd: +%.1f%%, Crit: +%.1f%%] (UID: %s)",
							it.W, q, it.Atk*100, it.Aspd*100, it.Crit*100, it.UID)
					}
				}
			}
		}
		d.mu.Unlock()

	case "pos":
		d.mu.Lock()
		d.pos.X = m.X
		d.pos.Z = m.Z
		d.vx = 0
		d.vz = 0
		d.mu.Unlock()

	case "joined":
		d.mu.Lock()
		z := m.Zone
		d.zone = &z
		d.mode = m.Mode
		d.slot = m.Slot
		d.combo = 0
		d.comboT = 0
		d.fishes = make(map[int64]*FishState)
		d.shots = make(map[int64]*ShotState)
		d.isSailing = false
		d.isEnteringZone = false
		d.zoneEnteredAt = time.Now()
		d.lastHarborTrip = time.Now()
		if d.partyLeader == "" {
			d.inParty = false
		}
		d.partyWaitingInHarbor = false
		d.mu.Unlock()

		zName := fmt.Sprintf("Zone %d", m.Zone)
		if m.Mode != "" {
			zName = fmt.Sprintf("Challenge [%s]", m.Mode)
		} else {
			d.defsLock.RLock()
			if m.Zone < len(d.defs.Zones) {
				zName = d.defs.Zones[m.Zone].Name
			}
			d.defsLock.RUnlock()
		}
		d.Log("ZONE_CHANGE", "Entered %s (Slot %d). Combat active!", zName, m.Slot)

	case "left":
		d.mu.Lock()
		curZ := -1
		if d.zone != nil {
			curZ = *d.zone
		}
		wasMode := d.mode
		d.zone = nil
		d.mode = ""
		d.slot = -1
		d.slotX = 0
		if len(m.Pos) >= 2 {
			d.pos.X = m.Pos[0]
			d.pos.Z = m.Pos[1]
		}
		d.vx = 0
		d.vz = 0
		d.fishes = make(map[int64]*FishState)
		d.shots = make(map[int64]*ShotState)
		d.isSailing = false
		d.isEnteringZone = false
		d.harborTicks = 0
		d.lastHarborTrip = time.Now()
		if m.Reason == "sunk" && curZ >= 0 {
			d.zoneDeaths[curZ]++
			d.lastDeathTime = time.Now()
		}
		if d.inParty && d.partyLeader != "" {
			d.partyWaitingInHarbor = true
			d.lastPartyActivity = time.Now()
		} else {
			d.inParty = false
			d.partyWaitingInHarbor = false
		}
		d.mu.Unlock()

		if m.Reason == "sunk" {
			d.Log("DEFENSE", "Vessel sank in Zone %d (server: sunk)! Total recorded sinks: %d. Returning to Harbor.", curZ, d.zoneDeaths[curZ])
		} else if wasMode != "" {
			d.Log("ZONE_CHANGE", "Exited challenge [%s], returned to Harbor.", wasMode)
		} else if d.inParty && d.partyLeader != "" {
			d.Log("PARTY", "Returned to Harbor with party. Waiting for leader '%s' to vote/regroup...", d.partyLeader)
		} else {
			d.Log("ZONE_CHANGE", "Left zone, returned to Harbor.")
		}

	case "s":
		d.handleSnapshot(m)

	case "kicked":
		d.mu.Lock()
		d.kicked = true
		d.kickWaitTill = time.Now().Add(25 * time.Second)
		d.mu.Unlock()
		d.Log("SYSTEM", "Received kick: '%s'. Waiting 25s before reconnecting...", m.Msg)

	case "partyAccept", "invite":
		if m.From != "" {
			d.Log("PARTY", "Incoming party invite from %s. Auto-accepting as carry!", m.From)
			_ = d.Send(map[string]interface{}{"t": "partyAccept", "name": m.From})
			d.mu.Lock()
			d.inParty = true
			d.partyLeader = m.From
			d.partyWaitingInHarbor = true
			d.partyJoinedAt = time.Now()
			d.lastPartyActivity = time.Now()
			inZone := d.zone != nil
			d.mu.Unlock()
			if inZone {
				d.Log("PARTY", "Leaving solo zone to Harbor to regroup with party leader %s...", m.From)
				_ = d.Send(map[string]interface{}{"t": "leaveZone"})
			}
		}

	case "vote":
		d.mu.Lock()
		d.inParty = true
		d.lastPartyActivity = time.Now()
		d.mu.Unlock()
		d.Log("PARTY", "Party area vote requested by leader. Auto-voting YES to carry!")
		_ = d.Send(map[string]interface{}{"t": "vote", "yes": true})

	case "party":
		d.handlePartyUpdate(m.Party)

	case "partyLeave", "partyDisband", "partyKick":
		d.mu.Lock()
		d.inParty = false
		d.partyWaitingInHarbor = false
		d.partyLeader = ""
		d.mu.Unlock()
		d.Log("PARTY", "Party ended (%s). Resuming solo progression.", m.T)

	case "market":
		d.handleMarketList(m.Listings)
	}
}

func (d *ReefBlastDaemon) handlePartyUpdate(data interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if data == nil {
		if d.inParty {
			d.Log("PARTY", "Party closed or left. Resuming solo progression.")
		}
		d.inParty = false
		d.partyLeader = ""
		d.partyWaitingInHarbor = false
		return
	}

	if b, ok := data.(bool); ok && !b {
		d.inParty = false
		d.partyLeader = ""
		d.partyWaitingInHarbor = false
		return
	}

	partyMap, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	membersRaw, hasMembers := partyMap["members"].([]interface{})
	if !hasMembers || len(membersRaw) <= 1 {
		if d.inParty {
			d.Log("PARTY", "Party has <= 1 member. Resuming solo progression.")
		}
		d.inParty = false
		d.partyLeader = ""
		d.partyWaitingInHarbor = false
		return
	}

	var foundLeader string
	if leaderStr, ok := partyMap["leader"].(string); ok && leaderStr != "" && leaderStr != d.botName {
		foundLeader = leaderStr
	} else {
		for _, mb := range membersRaw {
			if mbMap, ok := mb.(map[string]interface{}); ok {
				name, _ := mbMap["name"].(string)
				isLead, _ := mbMap["leader"].(bool)
				if name != "" && name != d.botName {
					if isLead {
						foundLeader = name
						break
					} else if foundLeader == "" {
						foundLeader = name
					}
				}
			} else if name, ok := mb.(string); ok && name != d.botName {
				if foundLeader == "" {
					foundLeader = name
				}
			}
		}
	}

	d.inParty = true
	d.lastPartyActivity = time.Now()
	if foundLeader != "" {
		d.partyLeader = foundLeader
	}
	d.Log("PARTY", "Active party confirmed with %d members (Leader: '%s'). Carry active!", len(membersRaw), d.partyLeader)
}

func (d *ReefBlastDaemon) handleSnapshotFast(data []byte) bool {
	if d.scanner == nil {
		d.scanner = NewSnapshotScanner()
	}
	if !d.scanner.Parse(data) {
		return false
	}

	now := time.Now().UnixMilli()

	d.mu.Lock()
	if d.zone == nil {
		d.mu.Unlock()
		return true
	}

	d.snapshotGen++

	// 1. Player state, combo & HP update
	for i := range d.scanner.Players {
		p := &d.scanner.Players[i]
		if p.Slot == d.slot {
			if p.Combo > d.combo {
				d.comboT = 2.0
				d.lastComboTime = time.Now()
			}
			d.combo = p.Combo
			if d.combo > d.stats.BestCombo {
				d.stats.BestCombo = d.combo
			}
			d.hp = p.HP

			// Check if vessel sank
			if p.IsDead {
				curZone := 0
				if d.zone != nil {
					curZone = *d.zone
				}
				d.zoneDeaths[curZone]++
				d.lastDeathTime = time.Now()
				d.Log("DEFENSE", "Vessel sank in Zone %d! Returning to harbor...", curZone)
				d.mu.Unlock()
				_ = d.Send(map[string]interface{}{"t": "leaveZone"})
				return true
			}
		}
	}

	// 2. Track fish with generation stamping (0 allocs)
	for i := range d.scanner.Fish {
		raw := &d.scanner.Fish[i]
		fid := raw.ID
		f, exists := d.fishes[fid]
		if !exists {
			d.fishes[fid] = &FishState{
				ID:          fid,
				Ty:          raw.Ty,
				X:           raw.X,
				Z:           raw.Z,
				TX:          raw.X,
				TZ:          raw.Z,
				HP:          raw.HP,
				Flags:       raw.Flags,
				Dir:         raw.Dir,
				LastT:       now,
				LastSeenGen: d.snapshotGen,
			}
		} else {
			dt := float64(now-f.LastT) / 1000.0
			if dt < 0.03 {
				dt = 0.03
			}
			curVx := (raw.X - f.TX) / dt
			curVz := (raw.Z - f.TZ) / dt
			if math.Hypot(curVx, curVz) > 0.01 {
				f.VX = f.VX*0.3 + curVx*0.7
				f.VZ = f.VZ*0.3 + curVz*0.7
			} else {
				f.VX = curVx
				f.VZ = curVz
			}
			f.X = raw.X
			f.Z = raw.Z
			f.TX = raw.X
			f.TZ = raw.Z
			f.HP = raw.HP
			f.Flags = raw.Flags
			f.Dir = raw.Dir
			f.LastT = now
			f.LastSeenGen = d.snapshotGen
		}
	}
	for fid, f := range d.fishes {
		if f.LastSeenGen != d.snapshotGen {
			delete(d.fishes, fid)
		}
	}

	// 3. Track incoming boss shots with generation stamping (0 allocs)
	for i := range d.scanner.Shots {
		raw := &d.scanner.Shots[i]
		sid := raw.ID
		shot, exists := d.shots[sid]
		if !exists {
			d.shots[sid] = &ShotState{
				ID:          sid,
				X:           raw.X,
				Z:           raw.Z,
				TX:          raw.X,
				TZ:          raw.Z,
				Kind:        raw.Kind,
				HP:          raw.HP,
				LastT:       now,
				LastSeenGen: d.snapshotGen,
			}
		} else {
			dt := float64(now-shot.LastT) / 1000.0
			if dt < 0.03 {
				dt = 0.03
			}
			curVx := (raw.X - shot.TX) / dt
			curVz := (raw.Z - shot.TZ) / dt
			if math.Hypot(curVx, curVz) > 0.01 {
				shot.VX = shot.VX*0.3 + curVx*0.7
				shot.VZ = shot.VZ*0.3 + curVz*0.7
			} else {
				shot.VX = curVx
				shot.VZ = curVz
			}
			shot.X = raw.X
			shot.Z = raw.Z
			shot.TX = raw.X
			shot.TZ = raw.Z
			shot.HP = raw.HP
			shot.LastT = now
			shot.LastSeenGen = d.snapshotGen
		}
	}
	for sid, shot := range d.shots {
		if shot.LastSeenGen != d.snapshotGen {
			delete(d.shots, sid)
		}
	}

	// 4. Handle events (kills, loot)
	if len(d.scanner.RawEvents) > 0 {
		for _, raw := range d.scanner.RawEvents {
			var ev struct {
				K    string          `json:"k"`
				S    int             `json:"s"`
				Loot [][]interface{} `json:"loot,omitempty"`
			}
			if err := json.Unmarshal(raw, &ev); err == nil {
				if ev.K == "k" {
					isMyKill := ev.S == d.slot
					if !isMyKill && len(ev.Loot) > 0 {
						for _, l := range ev.Loot {
							if len(l) >= 2 {
								if slotNum, ok := l[0].(float64); ok && int(slotNum) == d.slot {
									if coins, ok := l[1].(float64); ok && coins > 0 {
										isMyKill = true
										break
									}
								}
							}
						}
					}
					if isMyKill {
						d.stats.KillsThisSession++
					}
				}
			}
		}
	}

	d.mu.Unlock()
	d.combatAim()
	return true
}

func (d *ReefBlastDaemon) handleSnapshot(s *IncomingMessage) {
	now := time.Now().UnixMilli()

	d.mu.Lock()
	if d.zone == nil {
		d.mu.Unlock()
		return
	}

	d.snapshotGen++

	// 1. Player state, combo & HP update
	for _, p := range s.P {
		if len(p) > 6 {
			slotVal, _ := p[0].(float64)
			if int(slotVal) == d.slot {
				if len(p) > 4 {
					comboVal, _ := p[4].(float64)
					newCombo := int(comboVal)
					if newCombo > d.combo {
						d.comboT = 2.0
						d.lastComboTime = time.Now()
					}
					d.combo = newCombo
					if d.combo > d.stats.BestCombo {
						d.stats.BestCombo = d.combo
					}
				}

				if hpVal, ok := p[6].(float64); ok {
					d.hp = hpVal
				}

				// Check if vessel sank (p[11] is boolean dead)
				isSunk := false
				if len(p) > 11 {
					if sunk, ok := p[11].(bool); ok && sunk {
						isSunk = true
					}
				}
				if isSunk {
					curZone := 0
					if d.zone != nil {
						curZone = *d.zone
					}
					d.zoneDeaths[curZone]++
					d.lastDeathTime = time.Now()
					d.Log("DEFENSE", "Vessel sank in Zone %d! Returning to harbor...", curZone)
					d.mu.Unlock()
					_ = d.Send(map[string]interface{}{"t": "leaveZone"})
					return
				}
			}
		}
	}

	// 2. Track fish with generation stamping
	for _, raw := range s.F {
		if len(raw) >= 7 {
			id, _ := raw[0].(float64)
			fid := int64(id)

			ty, _ := raw[1].(float64)
			x, _ := raw[2].(float64)
			z, _ := raw[3].(float64)
			hp, _ := raw[4].(float64)
			flags, _ := raw[5].(float64)
			dir, _ := raw[6].(float64)

			f, exists := d.fishes[fid]
			if !exists {
				d.fishes[fid] = &FishState{
					ID:          fid,
					Ty:          int(ty),
					X:           x,
					Z:           z,
					TX:          x,
					TZ:          z,
					HP:          hp,
					Flags:       int(flags),
					Dir:         dir,
					LastT:       now,
					LastSeenGen: d.snapshotGen,
				}
			} else {
				dt := float64(now-f.LastT) / 1000.0
				if dt < 0.03 {
					dt = 0.03
				}
				f.VX = (x - f.TX) / dt
				f.VZ = (z - f.TZ) / dt
				f.X = x
				f.Z = z
				f.TX = x
				f.TZ = z
				f.HP = hp
				f.Flags = int(flags)
				f.Dir = dir
				f.LastT = now
				f.LastSeenGen = d.snapshotGen
			}
		}
	}
	for fid, f := range d.fishes {
		if f.LastSeenGen != d.snapshotGen {
			delete(d.fishes, fid)
		}
	}

	// 3. Track incoming boss shots with generation stamping
	for _, raw := range s.SH {
		if len(raw) >= 5 {
			id, _ := raw[0].(float64)
			sid := int64(id)

			x, _ := raw[1].(float64)
			z, _ := raw[2].(float64)
			kind, _ := raw[3].(string)
			hp, _ := raw[4].(float64)

			shot, exists := d.shots[sid]
			if !exists {
				d.shots[sid] = &ShotState{
					ID:          sid,
					X:           x,
					Z:           z,
					TX:          x,
					TZ:          z,
					Kind:        kind,
					HP:          hp,
					LastT:       now,
					LastSeenGen: d.snapshotGen,
				}
			} else {
				dt := float64(now-shot.LastT) / 1000.0
				if dt < 0.03 {
					dt = 0.03
				}
				shot.VX = (x - shot.TX) / dt
				shot.VZ = (z - shot.TZ) / dt
				shot.X = x
				shot.Z = z
				shot.TX = x
				shot.TZ = z
				shot.HP = hp
				shot.LastT = now
				shot.LastSeenGen = d.snapshotGen
			}
		}
	}
	for sid, shot := range d.shots {
		if shot.LastSeenGen != d.snapshotGen {
			delete(d.shots, sid)
		}
	}

	// 4. Handle events (kills, loot)
	if len(s.EV) > 0 {
		for _, raw := range s.EV {
			var ev struct {
				K    string          `json:"k"`
				S    int             `json:"s"`
				Loot [][]interface{} `json:"loot,omitempty"`
			}
			if err := json.Unmarshal(raw, &ev); err == nil {
				if ev.K == "k" {
					isMyKill := ev.S == d.slot
					if !isMyKill && len(ev.Loot) > 0 {
						for _, l := range ev.Loot {
							if len(l) >= 2 {
								if slotNum, ok := l[0].(float64); ok && int(slotNum) == d.slot {
									if coins, ok := l[1].(float64); ok && coins > 0 {
										isMyKill = true
										break
									}
								}
							}
						}
					}
					if isMyKill {
						d.stats.KillsThisSession++
					}
				}
			}
		}
	}

	d.mu.Unlock()
	d.combatAim()
}

func (d *ReefBlastDaemon) combatAim() {
	d.mu.Lock()
	if d.zone == nil || d.slot < 0 || d.me == nil {
		d.mu.Unlock()
		return
	}

	origin := Position{X: d.slotX, Z: 7.3}
	var eqDef *WeaponDef
	for _, it := range d.me.Items {
		if it.UID == d.me.Equipped {
			d.defsLock.RLock()
			for i := range d.defs.Weapons {
				if d.defs.Weapons[i].ID == it.W {
					eqDef = &d.defs.Weapons[i]
					break
				}
			}
			d.defsLock.RUnlock()
			break
		}
	}

	isHoming := false
	bulletSpeed := 25.0
	hasLeech := false
	if eqDef != nil {
		isHoming = eqDef.Kind == "homing"
		if eqDef.Speed > 0 {
			bulletSpeed = eqDef.Speed
		}
		if eqDef.Leech > 0 {
			hasLeech = true
		}
		for _, tr := range eqDef.Traits {
			if tr == "lifesteal" {
				hasLeech = true
			}
		}
	}

	// 1. OFFENSE: Target Selection
	var valid []*FishState
	for _, f := range d.fishes {
		// Ignore warded (Flags&8), shielded (Flags&1), vanished/stealthed (Flags&64), dead fish
		if (f.Flags&8) != 0 || (f.Flags&1) != 0 || (f.Flags&64) != 0 || f.HP <= 0 {
			continue
		}
		valid = append(valid, f)
	}

	// 2. DEFENSE: Intercept incoming boss torpedoes & DOOM ORBS
	// CRITICAL DEFENSE RULES:
	// A) Doom Orbs ("doom" or "1") inflict catastrophic doom damage: ALWAYS intercept with top priority!
	// B) If weapon does NOT have leech (e.g. levispread): boat CANNOT self-heal from fish!
	//    Intercept approaching torpedoes (Z > 0.0 or HP < 800) to protect the hull!
	// C) If weapon HAS leech: boat heals 1.2% per fish hit. Intercept if HP < 400 or no valid fish.
	if len(d.shots) > 0 {
		var doomOrbs []*ShotState
		var dangerous []*ShotState
		for _, s := range d.shots {
			isDoom := s.Kind == "doom" || s.Kind == "1"
			if isDoom {
				doomOrbs = append(doomOrbs, s)
			}
			if s.Z > -0.5 {
				dangerous = append(dangerous, s)
			}
		}

		// A: Always shoot down Doom Orbs first regardless of hull HP
		if len(doomOrbs) > 0 {
			sort.Slice(doomOrbs, func(i, j int) bool {
				return doomOrbs[i].Z > doomOrbs[j].Z
			})
			targetShot := doomOrbs[0]
			dist := math.Hypot(targetShot.X-origin.X, targetShot.Z-origin.Z)
			lead := math.Min(0.35, dist/bulletSpeed)
			aimX := math.Max(-16.5, math.Min(16.5, targetShot.X+targetShot.VX*lead))
			aimZ := math.Max(-10.5, math.Min(4.0, targetShot.Z+targetShot.VZ*lead))

			d.stats.ShotsIntercepted++
			d.mu.Unlock()
			d.pushAim(aimX, aimZ, true, true)
			return
		}

		// B & C: Torpedo defense threshold based on lifesteal capability
		interceptThreshold := 400.0
		dangerZ := 0.5
		if !hasLeech {
			interceptThreshold = 800.0
			dangerZ = 0.0
		}

		shouldIntercept := (d.hp > 0 && d.hp < interceptThreshold) || len(valid) == 0
		if shouldIntercept && len(dangerous) > 0 {
			var filtered []*ShotState
			for _, s := range dangerous {
				if s.Z > dangerZ {
					filtered = append(filtered, s)
				}
			}
			if len(filtered) > 0 {
				sort.Slice(filtered, func(i, j int) bool {
					return filtered[i].Z > filtered[j].Z
				})
				targetShot := filtered[0]
				dist := math.Hypot(targetShot.X-origin.X, targetShot.Z-origin.Z)
				lead := math.Min(0.35, dist/bulletSpeed)
				aimX := math.Max(-16.5, math.Min(16.5, targetShot.X+targetShot.VX*lead))
				aimZ := math.Max(-10.5, math.Min(4.0, targetShot.Z+targetShot.VZ*lead))

				d.stats.ShotsIntercepted++
				d.mu.Unlock()
				d.pushAim(aimX, aimZ, true, true)
				return
			}
		}
	}

	if len(valid) == 0 {
		d.mu.Unlock()
		d.pushAim(0, -4, false, false)
		return
	}

	comboCritical := d.combo >= 3 && d.comboT < 0.85
	var target *FishState

	if comboCritical {
		sort.Slice(valid, func(i, j int) bool {
			distA := math.Hypot(valid[i].X-origin.X, valid[i].Z-origin.Z)
			distB := math.Hypot(valid[j].X-origin.X, valid[j].Z-origin.Z)
			return (valid[i].HP*2.0 + distA) < (valid[j].HP*2.0 + distB)
		})
		target = valid[0]
	} else {
		d.defsLock.RLock()
		sort.Slice(valid, func(i, j int) bool {
			var bossA, bossB int
			if valid[i].Ty < len(d.defs.Fish) {
				fDef := d.defs.Fish[valid[i].Ty]
				if fDef.Boss || fDef.Mini {
					bossA = 1
				}
			}
			if valid[j].Ty < len(d.defs.Fish) {
				fDef := d.defs.Fish[valid[j].Ty]
				if fDef.Boss || fDef.Mini {
					bossB = 1
				}
			}
			if bossA != bossB {
				return bossA > bossB
			}
			distA := math.Hypot(valid[i].X-origin.X, valid[i].Z-origin.Z)
			distB := math.Hypot(valid[j].X-origin.X, valid[j].Z-origin.Z)
			if valid[i].HP != valid[j].HP {
				return valid[i].HP < valid[j].HP
			}
			return distA < distB
		})
		d.defsLock.RUnlock()
		target = valid[0]
	}

	aimX := target.X
	aimZ := target.Z
	if !isHoming {
		dist := math.Hypot(target.X-origin.X, target.Z-origin.Z)
		flightTime := dist / bulletSpeed
		aimX += target.VX * flightTime
		aimZ += target.VZ * flightTime
	}

	aimX = math.Max(-16.5, math.Min(16.5, aimX))
	aimZ = math.Max(-10.5, math.Min(4.0, aimZ))

	d.mu.Unlock()
	d.pushAim(aimX, aimZ, true, false)
}

func (d *ReefBlastDaemon) pushAim(x, z float64, firing bool, emergency bool) {
	now := time.Now().UnixMilli()
	if !d.aimInitialized {
		d.smoothAimX = x
		d.smoothAimZ = z
		d.aimInitialized = true
	}

	var outX, outZ float64
	if emergency {
		// Immediate snap for Doom Orb & lethal torpedo defense
		d.smoothAimX = x
		d.smoothAimZ = z
		outX = x
		outZ = z
	} else if !firing {
		// Idle rest smoothing
		d.smoothAimX += (x - d.smoothAimX) * 0.4
		d.smoothAimZ += (z - d.smoothAimZ) * 0.4
		outX = d.smoothAimX
		outZ = d.smoothAimZ
	} else {
		// Human tracking: rapid flick on target switch, smooth follow on movement
		dist := math.Hypot(x-d.smoothAimX, z-d.smoothAimZ)
		alpha := 0.58
		if dist > 6.0 {
			alpha = 0.82 // fast human flick
		}
		d.smoothAimX += (x - d.smoothAimX) * alpha
		d.smoothAimZ += (z - d.smoothAimZ) * alpha

		// Subtle hand micro-tremor around target center (within fish hitbox)
		tSec := float64(now) / 1000.0
		tremorAmp := 0.12
		outX = d.smoothAimX + tremorAmp*math.Sin(2.7*tSec+d.personaSeed)
		outZ = d.smoothAimZ + tremorAmp*math.Cos(3.1*tSec+d.personaSeed*1.4)
	}

	outX = math.Max(-16.5, math.Min(16.5, outX))
	outZ = math.Max(-10.5, math.Min(4.0, outZ))

	moved := math.Abs(outX-d.lastAimSent.x)+math.Abs(outZ-d.lastAimSent.z) > 0.08
	timeDiff := now - d.lastAimSent.t

	if firing != d.lastAimSent.f || (timeDiff > 50 && (moved || timeDiff > 400)) {
		_ = d.sendAimRaw(outX, outZ, firing)
		d.lastAimSent.x = outX
		d.lastAimSent.z = outZ
		d.lastAimSent.f = firing
		d.lastAimSent.t = now
	}
}

// getWaypoints generates a smooth organic path around Island 0 (0, -32) or along open water
func (d *ReefBlastDaemon) getWaypoints(startX, startZ, targetX, targetZ float64, seed float64) [][2]float64 {
	dx := targetX - startX
	dz := targetZ - startZ
	lenSq := dx*dx + dz*dz
	dist := math.Sqrt(lenSq)

	needsAvoidance := false
	if lenSq > 1.0 {
		t := ((0.0 - startX)*dx + (-32.0 - startZ)*dz) / lenSq
		if t > 0.05 && t < 0.95 {
			projX := startX + t*dx
			projZ := startZ + t*dz
			distToCenter := math.Hypot(projX-0.0, projZ-(-32.0))
			if distToCenter < 24.0 {
				needsAvoidance = true
			}
		}
	}

	if needsAvoidance {
		// Subtle organic variation in island avoidance channel
		wpVariance := math.Sin(seed*1.1) * 2.0
		waypointX := 28.0 + wpVariance
		if (startX+targetX)/2.0 < 0 {
			waypointX = -28.0 - wpVariance
		}
		return [][2]float64{
			{waypointX, -32.0},
			{targetX, targetZ},
		}
	}

	// Long open-water voyages (> 38 units, e.g. heading to Zone Gates or Boss Rush):
	// Add gentle curved mid-point waypoint for a natural naval arc
	if dist > 38.0 {
		midX := (startX + targetX) / 2.0
		midZ := (startZ + targetZ) / 2.0
		perpX := -dz / dist
		perpZ := dx / dist

		curveOffset := math.Sin(seed*0.7) * 3.5
		candX := midX + perpX*curveOffset
		candZ := midZ + perpZ*curveOffset

		// Verify mid-waypoint doesn't encroach into Island 0 (r=17, safety r=26)
		if math.Hypot(candX-0.0, candZ-(-32.0)) > 26.0 {
			return [][2]float64{
				{candX, candZ},
				{targetX, targetZ},
			}
		}
	}

	return [][2]float64{
		{targetX, targetZ},
	}
}

// stepHarborPhysics accurately simulates client-side harbor boat physics and collision sliding
func (d *ReefBlastDaemon) stepHarborPhysics(dt float64, ix, iz float64, speed float64) (float64, float64, float64) {
	hypotLen := math.Hypot(ix, iz)
	tvx := 0.0
	tvz := 0.0
	if hypotLen > 0.001 {
		tvx = (ix / hypotLen) * speed
		tvz = (iz / hypotLen) * speed
	}

	damping := 1.4
	if hypotLen > 0.001 {
		damping = 2.4
	}
	k := 1.0 - math.Exp(-dt*damping)
	d.vx += (tvx - d.vx) * k
	d.vz += (tvz - d.vz) * k

	d.pos.X += d.vx * dt
	d.pos.Z += d.vz * dt

	var islands []HarborIsland
	var areas []HarborArea
	radius := 205.0

	d.defsLock.RLock()
	if d.defs != nil {
		islands = d.defs.Harbor.Islands
		areas = d.defs.Harbor.Areas
		if d.defs.Harbor.Radius > 0 {
			radius = d.defs.Harbor.Radius
		}
	}
	d.defsLock.RUnlock()

	if len(islands) == 0 {
		islands = []HarborIsland{{X: 0, Z: -32, R: 17}}
	}
	if len(areas) == 0 {
		areas = []HarborArea{
			{ID: "armory", X: -26, Z: -12, R: 8},
			{ID: "tavern", X: -9, Z: -6, R: 8},
			{ID: "market", X: 9, Z: -6, R: 8},
			{ID: "shipyard", X: 26, Z: -12, R: 8},
		}
	}

	// 1. Island collision sliding
	for _, is := range islands {
		dx := d.pos.X - is.X
		dz := d.pos.Z - is.Z
		dist := math.Hypot(dx, dz)
		minDist := is.R*1.1 + 1.5
		if dist < minDist && dist > 0.001 {
			d.pos.X = is.X + (dx/dist)*minDist
			d.pos.Z = is.Z + (dz/dist)*minDist
		}
	}

	// 2. Dock solid collision sliding
	for _, a := range areas {
		cx := a.X
		cz := a.Z - 1.5
		diffX := math.Abs(d.pos.X - cx)
		diffZ := math.Abs(d.pos.Z - cz)
		if diffX < 6.5 && diffZ < 4.8 {
			ox := 6.5 - diffX
			oz := 4.8 - diffZ
			if ox < oz {
				sign := 1.0
				if d.pos.X < cx {
					sign = -1.0
				}
				d.pos.X += sign * ox
			} else {
				sign := 1.0
				if d.pos.Z < cz {
					sign = -1.0
				}
				d.pos.Z += sign * oz
			}
		}
	}

	// 3. Harbor radius boundary
	distOrigin := math.Hypot(d.pos.X, d.pos.Z)
	if distOrigin > radius && distOrigin > 0.001 {
		d.pos.X *= radius / distOrigin
		d.pos.Z *= radius / distOrigin
	}

	// 4. Smooth angular turning towards velocity
	sp := math.Hypot(d.vx, d.vz)
	if sp > 0.4 {
		wantR := math.Atan2(d.vx, d.vz)
		diffR := math.Atan2(math.Sin(wantR-d.pos.R), math.Cos(wantR-d.pos.R))
		d.pos.R += diffR * math.Min(1.0, dt*5.0)
	}

	return d.pos.X, d.pos.Z, d.pos.R
}

func (d *ReefBlastDaemon) SailTo(targetX, targetZ float64, stopRadius float64, maxDuration time.Duration) {
	d.mu.Lock()
	d.isSailing = true
	startX := d.pos.X
	startZ := d.pos.Z

	// Compute bot personality seed for organic movement variation
	seed := float64(0)
	nameStr := d.botName
	if d.me != nil && d.me.Name != "" {
		nameStr = d.me.Name
	}
	for _, c := range nameStr {
		seed += float64(c) * 7.13
	}
	voyageSeed := seed + float64(time.Now().Unix()%3600)

	// Determine boat speed from defs
	baseSpeed := 9.5
	if d.me != nil {
		d.defsLock.RLock()
		for _, b := range d.defs.Boats {
			if b.ID == d.me.Boat {
				baseSpeed = b.Speed
				break
			}
		}
		d.defsLock.RUnlock()
	}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.isSailing = false
		d.vx = 0
		d.vz = 0
		d.mu.Unlock()
	}()

	// 1. Organic landing variation for docks (stopRadius <= 4.0)
	actualTargetX := targetX
	actualTargetZ := targetZ
	if stopRadius <= 4.0 {
		dockJitterX := math.Sin(voyageSeed*1.31) * 0.85
		dockJitterZ := math.Cos(voyageSeed*0.87) * 0.50
		actualTargetX += dockJitterX
		actualTargetZ += dockJitterZ
	}

	waypoints := d.getWaypoints(startX, startZ, actualTargetX, actualTargetZ, voyageSeed)
	start := time.Now()
	dt := 0.05 // 50ms physics sub-tick (20 Hz)
	lastSend := time.Now()

	// Wave dynamics parameters
	omega := 1.9 + math.Mod(seed, 0.45) // ocean wave sway frequency (~2.0 rad/s)
	phi := math.Mod(seed, 2*math.Pi)    // bot-specific wave phase offset

	for wpIdx, wp := range waypoints {
		wpX, wpZ := wp[0], wp[1]
		isFinal := (wpIdx == len(waypoints)-1)
		wpStopRadius := stopRadius
		if !isFinal {
			wpStopRadius = 5.0
		}

		for time.Since(start) < maxDuration {
			d.mu.Lock()
			dx := wpX - d.pos.X
			dz := wpZ - d.pos.Z
			dist := math.Hypot(dx, dz)

			if dist <= wpStopRadius {
				d.mu.Unlock()
				break
			}

			// Base unit direction vector
			ux := dx / dist
			uz := dz / dist

			elapsed := time.Since(start).Seconds()

			// 2. Hydrodynamic wave sway & rudder correction (multi-harmonic ocean chop)
			swayAmp := 0.09
			if isFinal && dist < 7.5 {
				swayAmp *= (dist / 7.5) // Fades sway smoothly on final docking approach
			}
			sway := swayAmp * (0.75*math.Sin(omega*elapsed+phi) + 0.25*math.Sin(2.3*omega*elapsed+phi*1.4))

			// Steer input vector with perpendicular wave sway
			ix := ux + sway*(-uz)
			iz := uz + sway*(ux)
			iLen := math.Hypot(ix, iz)
			if iLen > 0.001 {
				ix /= iLen
				iz /= iLen
			}

			// 3. Organic throttle modulation (gentle surge & coasting glide)
			throttle := 0.96 + 0.06*(0.5+0.5*math.Sin(1.15*elapsed+phi))
			if isFinal && dist < 6.5 {
				approachFactor := math.Max(0.42, dist/6.0)
				throttle *= approachFactor
			}
			currSpeed := baseSpeed * throttle

			curX, curZ, curR := d.stepHarborPhysics(dt, ix, iz, currSpeed)
			d.mu.Unlock()

			// Send 'mv' packet at 10 Hz (every 100ms) with zero allocations
			now := time.Now()
			if now.Sub(lastSend) >= 100*time.Millisecond {
				lastSend = now
				_ = d.sendMvRaw(curX, curZ, curR)
			}

			time.Sleep(50 * time.Millisecond)
		}
	}

	// 4. Organic water drift & glide deceleration to rest (settling momentum)
	for i := 0; i < 5; i++ {
		d.mu.Lock()
		curX, curZ, curR := d.stepHarborPhysics(dt, 0, 0, baseSpeed)
		d.mu.Unlock()
		if i == 2 {
			_ = d.sendMvRaw(curX, curZ, curR)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Send final resting position
	d.mu.Lock()
	finalX := math.Round(d.pos.X*100) / 100
	finalZ := math.Round(d.pos.Z*100) / 100
	finalR := math.Round(d.pos.R*100) / 100
	d.vx = 0
	d.vz = 0
	d.mu.Unlock()

	_ = d.sendMvRaw(finalX, finalZ, finalR)
}

func (d *ReefBlastDaemon) managerLoop() {
	// Seeded PRNG for organic human desynchronization
	seed := int64(0)
	for _, c := range d.botName {
		seed = seed*31 + int64(c)
	}
	seed ^= time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))

	for {
		// Desynchronized human pacing: 2200ms - 3800ms
		jitterMs := 2200 + rng.Intn(1600)
		select {
		case <-d.stopChan:
			return
		case <-time.After(time.Duration(jitterMs) * time.Millisecond):
		}
		d.managerStep()
	}
}

func (d *ReefBlastDaemon) managerStep() {
	d.mu.Lock()
	if !d.connected || d.me == nil || d.managerBusy {
		d.mu.Unlock()
		return
	}
	if d.isEnteringZone {
		if time.Since(d.enteringZoneAt) < 6*time.Second {
			d.mu.Unlock()
			return
		}
		d.isEnteringZone = false
	}
	d.managerBusy = true
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.managerBusy = false
		d.mu.Unlock()
	}()

	d.readPolicy()

	d.mu.Lock()
	inParty := d.inParty
	waitingInHarbor := d.partyWaitingInHarbor
	lastPartyAct := d.lastPartyActivity
	leader := d.partyLeader
	if d.checkNeedsRespecLocked() {
		d.needsRespec = true
	}
	d.mu.Unlock()

	// Party carry timeout: if waiting in Harbor for > 5 mins with no leader vote/action, or if leader is empty, release party lock
	if inParty && waitingInHarbor && (leader == "" || time.Since(lastPartyAct) > 5*time.Minute) {
		d.mu.Lock()
		d.inParty = false
		d.partyWaitingInHarbor = false
		d.partyLeader = ""
		d.mu.Unlock()
		if leader != "" {
			d.Log("PARTY", "Party idle timeout (5m with no leader action from %s). Resuming independent solo progression.", leader)
		}
		inParty = false
		leader = ""
	}

	// 0. Respec if requested (only when solo, never interrupt an active party carry)
	if d.needsRespec && !inParty {
		d.mu.Lock()
		canAfford := d.me == nil || d.me.RespecCost == 0 || d.me.Coins >= d.me.RespecCost
		inZone := d.zone != nil
		d.mu.Unlock()
		if !canAfford {
			d.needsRespec = false
		} else if inZone {
			d.Log("SKILL", "Returning to Harbor to align skill tree with AM-GM curve...")
			_ = d.Send(map[string]interface{}{"t": "leaveZone"})
			return
		} else {
			d.Log("SKILL", "Resetting skills for optimal AM-GM allocation...")
			_ = d.Send(map[string]interface{}{"t": "respec"})
			d.needsRespec = false
			d.hasAutoRespecced = true
			time.Sleep(300 * time.Millisecond)
		}
	}

	// 1. AM-GM Skills
	d.autoAllocateSkills()

	// 2. Claim daily quests
	d.claimDailyQuests()

	// 3. Auto-equip best weapon, boat & helper
	d.autoEquipBestWeapon()
	d.autoEquipBestBoat()
	d.autoEquipBestHelper()

	// 4. Harbor / Sea operations
	d.mu.Lock()
	inZone := d.zone != nil
	curZone := -1
	if inZone {
		curZone = *d.zone
	}
	d.mu.Unlock()

	if !inZone {
		if inParty && leader != "" {
			d.manageHarborPartyCarry()
			return
		}
		d.manageHarbor()
	} else {
		// Combat voyage management
		if inParty && leader != "" {
			// In party carry mode: NEVER voluntarily leave the zone!
			// Stay in the zone carrying the team until the voyage ends or the vessel sinks.
			return
		}
		timeAtSea := time.Since(d.zoneEnteredAt).Seconds()
		bestZone := d.GetBestZone()
		outgrownZone := curZone < bestZone
		hasUpgrade := d.hasPendingUpgrades()
		routineShop := hasUpgrade && timeAtSea > 300
		maxLimit := timeAtSea > 900 // 15-minute safety check

		if (outgrownZone || routineShop || maxLimit) && timeAtSea > 20 {
			reason := "routine maintenance"
			if outgrownZone {
				reason = fmt.Sprintf("advancing to Zone %d", bestZone)
			} else if hasUpgrade {
				reason = "purchasing available equipment upgrades"
			}
			d.Log("UPGRADE", "Harbor trip initiated (%s, Zone: %d vs Best: %d, Time at sea: %.0fs). Returning to Harbor...", reason, curZone, bestZone, timeAtSea)
			d.lastHarborTrip = time.Now()
			_ = d.Send(map[string]interface{}{"t": "leaveZone"})
		}
	}
}

func (d *ReefBlastDaemon) manageHarborPartyCarry() {
	d.mu.Lock()
	leader := d.partyLeader
	inParty := d.inParty
	d.mu.Unlock()

	if !inParty || leader == "" {
		d.mu.Lock()
		d.inParty = false
		d.partyLeader = ""
		d.partyWaitingInHarbor = false
		d.mu.Unlock()
		d.manageHarbor()
		return
	}

	d.harborTicks++

	if d.checkHullUpgrade() {
		return
	}
	if d.checkForgeCrafting() {
		return
	}
	if d.checkHelperRecruitment() {
		return
	}
	if d.checkMarketSelling() {
		return
	}

	if d.policy.AllowSalvaging {
		d.autoSalvageJunk()
	}

	if d.harborTicks%10 == 1 {
		d.Log("PARTY", "Holding position in Harbor in Carry Mode. Waiting for party leader '%s' to propose area vote...", leader)
	}
}

func (d *ReefBlastDaemon) manageHarbor() {
	d.harborTicks++

	// 1. Check if daily quest requires island challenge (Boss Rush)
	if d.checkNeedsIslandRun() {
		d.Log("QUEST", "Daily quest requires island challenge! Setting sail for Boss Rush islet at (-130.9, -102.3)...")
		d.SailTo(-130.9, -102.3, 5.0, 10*time.Second)
		d.mu.Lock()
		d.isEnteringZone = true
		d.enteringZoneAt = time.Now()
		d.mu.Unlock()
		d.Log("ZONE_CHANGE", "Entering Boss Rush challenge...")
		_ = d.Send(map[string]interface{}{"t": "modeEnter", "mode": "rush", "tier": 0})
		return
	}

	// 2. Need-Based Harbor Docks (only visited if an actionable upgrade exists)
	// Priority: Vessel Hull -> Weapon Craft -> Crew Helper -> Surplus Selling
	if d.checkHullUpgrade() {
		return
	}
	if d.checkForgeCrafting() {
		return
	}
	if d.checkHelperRecruitment() {
		return
	}
	if d.checkMarketSelling() {
		return
	}

	// Auto-salvage junk if policy allows (doesn't require sailing to dock)
	if d.policy.AllowSalvaging {
		d.autoSalvageJunk()
	}

	// 3. Periodic Market Inspection (Trader Persona or rare periodic check)
	// Real players only check the market occasionally, not every single respawn
	if d.policy.AllowBuying && d.me != nil {
		isTrader := (d.persona == 1)
		timeSinceMarket := time.Since(d.lastMarketPoll)
		shouldBrowse := (isTrader && timeSinceMarket > 8*time.Minute) || (d.me.Coins > 100000 && timeSinceMarket > 20*time.Minute)
		if shouldBrowse {
			d.marketSniped = false
			d.lastMarketPoll = time.Now()
			d.Log("HARBOR", "Sailing to Market dock (9, -1) to inspect listings...")
			d.SailTo(9, -1.0, 3.5, 6*time.Second)
			time.Sleep(time.Duration(1200+int(d.personaSeed)%1000) * time.Millisecond)
			_ = d.Send(map[string]interface{}{"t": "mkList"})
			return
		}
	}

	// 4. Explorer Persona: Occasional scenic harbor cruise (~10% chance)
	if d.harborTicks == 1 && d.persona == 2 {
		rVal := int(time.Now().Unix()+int64(d.personaSeed)) % 10
		if rVal == 0 {
			d.Log("CRUISE", "Explorer persona taking a brief scenic harbor canal sweep...")
			d.SailTo(0, 5.0, 4.0, 4*time.Second)
		}
	}

	// 5. Direct Transit to Level-Appropriate Zone Gate (~75% of trips!)
	targetZone := d.GetBestZone()
	var gate *HarborGate
	d.defsLock.RLock()
	for i := range d.defs.Harbor.Gates {
		if d.defs.Harbor.Gates[i].Zone == targetZone {
			g := d.defs.Harbor.Gates[i]
			gate = &g
			break
		}
	}
	zName := fmt.Sprintf("Zone %d", targetZone)
	if targetZone < len(d.defs.Zones) {
		zName = d.defs.Zones[targetZone].Name
	}
	d.defsLock.RUnlock()

	if gate != nil {
		d.Log("ZONE_CHANGE", "Sailing towards Zone %d Gate at (%.1f, %.1f)...", targetZone, gate.X, gate.Z)
		d.SailTo(gate.X, gate.Z, 5.0, 8*time.Second)
	}

	// Human departure hesitation at gate portal
	hesitationMs := 400 + int(d.personaSeed)%600
	time.Sleep(time.Duration(hesitationMs) * time.Millisecond)

	d.mu.Lock()
	d.isEnteringZone = true
	d.enteringZoneAt = time.Now()
	d.mu.Unlock()

	d.Log("ZONE_CHANGE", "Setting sail into Zone %d: %s...", targetZone, zName)
	_ = d.Send(map[string]interface{}{"t": "enter", "zone": targetZone})
}

// GetBestZone dynamically evaluates level-appropriate unlocked zones
func (d *ReefBlastDaemon) GetBestZone() int {
	d.mu.Lock()
	playerLevel := 1
	if d.me != nil {
		playerLevel = d.me.Level
	}
	d.mu.Unlock()

	highest := 0
	d.defsLock.RLock()
	defer d.defsLock.RUnlock()

	for z, zDef := range d.defs.Zones {
		if deaths, ok := d.zoneDeaths[z]; ok && deaths >= 3 && time.Since(d.lastDeathTime) < 3*time.Minute {
			continue
		}
		if playerLevel >= zDef.Level && z > highest {
			highest = z
		}
	}
	return highest
}

func (d *ReefBlastDaemon) CalcWeaponScore(wDef *WeaponDef, item *Item) float64 {
	if wDef == nil {
		return -1
	}
	effTier := float64(wDef.Tier)
	if wDef.Exclusive {
		effTier = 6.5
	}

	baseDps := wDef.DPS
	if wDef.Scales && len(d.defs.TierDPS) > 0 {
		lvl := 100
		if d.me != nil {
			lvl = d.me.Level
		}
		t := int(math.Min(6, float64(lvl/16)+1))
		if t < len(d.defs.TierDPS) {
			edge := d.defs.ExclusiveEdge
			if edge <= 0 {
				edge = 1.08
			}
			baseDps = d.defs.TierDPS[t] * edge
		}
	}
	if baseDps == 0 && wDef.Damage > 0 && wDef.FireRate > 0 {
		baseDps = wDef.Damage * (1000.0 / wDef.FireRate)
	}

	rollAtk := 1.0
	rollAspd := 1.0
	rollCrit := 1.0
	if item != nil {
		rollAtk += item.Atk
		rollAspd += item.Aspd
		rollCrit += item.Crit
	}
	effectiveDps := baseDps * rollAtk * rollAspd * rollCrit

	traitMult := 1.0
	for _, tr := range wDef.Traits {
		switch tr {
		case "homing":
			traitMult += 0.15
		case "explosive":
			traitMult += 0.12
		case "chain":
			traitMult += 0.10
		case "lifesteal":
			traitMult += 0.14
		case "poison":
			traitMult += 0.12
		case "burn":
			traitMult += 0.10
		case "pierce":
			traitMult += 0.08
		case "spread":
			traitMult += 0.06
		}
	}

	plBonus := float64(wDef.PL) * 0.1
	return (effTier * 2000.0) + (effectiveDps * traitMult) + plBonus
}

func (d *ReefBlastDaemon) CalcBoatScore(bDef *BoatDef) float64 {
	if bDef == nil {
		return -1
	}
	effTier := float64(bDef.Tier)
	if bDef.Exclusive {
		effTier = 6.5
	}

	ehp := bDef.HP * (1.0 + (bDef.Def / 100.0))
	dmgBuff := bDef.Buffs["atk"] * 1000000.0
	aspdBuff := bDef.Buffs["aspd"] * 1000000.0
	critBuff := bDef.Buffs["crit"] * 800000.0
	regenBuff := bDef.Buffs["regen"] * 2000000.0
	econBuff := (bDef.Buffs["coin"] + bDef.Buffs["xp"]) * 10000.0
	plScore := float64(bDef.PL) * 2.0

	return (effTier * 20000.0) + (ehp * 0.5) + dmgBuff + aspdBuff + critBuff + regenBuff + econBuff + plScore
}

func (d *ReefBlastDaemon) CalcHelperScore(hDef *HelperDef) float64 {
	if hDef == nil {
		return -1
	}
	return (hDef.DPS * 10.0) + float64(hDef.PL)
}

// CalcItemQuality calculates official Reefblast roll quality percentage (0% to 100%)
// matching the game client formula: average of (v - min) / (max - min) across ATK, ASPD, CRIT rolls.
func (d *ReefBlastDaemon) CalcItemQuality(it *Item) int {
	if it == nil || it.Bound {
		return 0
	}
	d.defsLock.RLock()
	rolls := d.defs.Rolls
	d.defsLock.RUnlock()

	minAtk, maxAtk := -0.2, 0.5
	minAspd, maxAspd := -0.2, 0.5
	minCrit, maxCrit := -0.05, 0.15

	if len(rolls) >= 3 {
		minAtk, maxAtk = rolls[0].Min, rolls[0].Max
		minAspd, maxAspd = rolls[1].Min, rolls[1].Max
		minCrit, maxCrit = rolls[2].Min, rolls[2].Max
	}

	barAtk := (it.Atk - minAtk) / (maxAtk - minAtk)
	barAspd := (it.Aspd - minAspd) / (maxAspd - minAspd)
	barCrit := (it.Crit - minCrit) / (maxCrit - minCrit)

	q := math.Round((barAtk + barAspd + barCrit) / 3.0 * 100.0)
	if q < 0 {
		q = 0
	} else if q > 100 {
		q = 100
	}
	return int(q)
}

func (d *ReefBlastDaemon) LogWeaponArsenal() {
	d.mu.Lock()
	if d.me == nil || len(d.me.Items) == 0 {
		d.mu.Unlock()
		return
	}
	items := make([]Item, len(d.me.Items))
	copy(items, d.me.Items)
	equippedUID := d.me.Equipped
	d.mu.Unlock()

	d.Log("GEAR", "=== Weapon Arsenal Quality Telemetry (%d weapons) ===", len(items))
	for i, it := range items {
		q := d.CalcItemQuality(&it)
		eqTag := ""
		if it.UID == equippedUID {
			eqTag = " [EQUIPPED 🎯]"
		}
		if it.Bound {
			d.Log("GEAR", " #%d: %s%s (Starter Blaster · Bound)", i+1, it.W, eqTag)
		} else {
			d.Log("GEAR", " #%d: %s%s - Quality: %d%% [Atk: +%.1f%%, Aspd: +%.1f%%, Crit: +%.1f%%] (UID: %s)",
				i+1, it.W, eqTag, q, it.Atk*100, it.Aspd*100, it.Crit*100, it.UID)
		}
	}
}

func (d *ReefBlastDaemon) autoEquipBestWeapon() {
	d.mu.Lock()
	if d.me == nil || len(d.me.Items) == 0 {
		d.mu.Unlock()
		return
	}

	var bestItem *Item
	var bestScore float64 = -1
	d.defsLock.RLock()
	for i := range d.me.Items {
		item := &d.me.Items[i]
		item.Quality = d.CalcItemQuality(item)
		for j := range d.defs.Weapons {
			if d.defs.Weapons[j].ID == item.W {
				score := d.CalcWeaponScore(&d.defs.Weapons[j], item)
				if score > bestScore {
					bestScore = score
					bestItem = item
				}
				break
			}
		}
	}
	d.defsLock.RUnlock()

	if bestItem != nil && bestItem.UID != d.me.Equipped {
		q := d.CalcItemQuality(bestItem)
		bestItem.Quality = q
		d.Log("GEAR", "Switched weapon to: %s (Quality: %d%%, UID: %s, Score: %.0f) [Atk: +%.1f%%, Aspd: +%.1f%%, Crit: +%.1f%%]",
			bestItem.W, q, bestItem.UID, bestScore, bestItem.Atk*100, bestItem.Aspd*100, bestItem.Crit*100)
		_ = d.Send(map[string]interface{}{"t": "equipWeapon", "id": bestItem.UID})
		d.me.Equipped = bestItem.UID
	}
	d.mu.Unlock()
}

func (d *ReefBlastDaemon) autoEquipBestBoat() {
	d.mu.Lock()
	if d.me == nil || len(d.me.Boats) <= 1 {
		d.mu.Unlock()
		return
	}

	bestBoat := d.me.Boat
	var bestScore float64 = -1
	d.defsLock.RLock()
	for _, bID := range d.me.Boats {
		for i := range d.defs.Boats {
			if d.defs.Boats[i].ID == bID {
				score := d.CalcBoatScore(&d.defs.Boats[i])
				if score > bestScore {
					bestScore = score
					bestBoat = bID
				}
				break
			}
		}
	}
	d.defsLock.RUnlock()

	if bestBoat != "" && bestBoat != d.me.Boat {
		d.Log("GEAR", "Switched vessel to: %s (Score: %.0f)", bestBoat, bestScore)
		_ = d.Send(map[string]interface{}{"t": "equipBoat", "id": bestBoat})
		d.me.Boat = bestBoat
	}
	d.mu.Unlock()
}

func (d *ReefBlastDaemon) autoEquipBestHelper() {
	d.mu.Lock()
	if d.me == nil || len(d.me.Helpers) <= 1 {
		d.mu.Unlock()
		return
	}

	bestHelper := d.me.Helper
	var bestScore float64 = -1
	d.defsLock.RLock()
	for _, hID := range d.me.Helpers {
		for i := range d.defs.Helpers {
			if d.defs.Helpers[i].ID == hID {
				score := d.CalcHelperScore(&d.defs.Helpers[i])
				if score > bestScore {
					bestScore = score
					bestHelper = hID
				}
				break
			}
		}
	}
	d.defsLock.RUnlock()

	if bestHelper != "" && bestHelper != d.me.Helper {
		d.Log("GEAR", "Switched crew helper to: %s (Score: %.0f)", bestHelper, bestScore)
		_ = d.Send(map[string]interface{}{"t": "equipHelper", "id": bestHelper})
		d.me.Helper = bestHelper
	}
	d.mu.Unlock()
}

// hasPendingUpgrades dynamically checks if there are actionable upgrades waiting in Harbor
func (d *ReefBlastDaemon) hasPendingUpgrades() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.me == nil {
		return false
	}

	// 1. Check if respec is required in Harbor
	if d.needsRespec {
		return true
	}

	// 2. Check if inventory has surplus items to salvage or sell (> 4 items)
	if (d.policy.AllowSalvaging || d.policy.AllowMarketSelling) && len(d.me.Items) >= 5 {
		return true
	}

	if !d.policy.AllowBuying {
		return false
	}

	d.defsLock.RLock()
	defer d.defsLock.RUnlock()

	// 3. Check if a superior boat hull is affordable
	ownedBoats := make(map[string]bool)
	for _, b := range d.me.Boats {
		ownedBoats[b] = true
	}
	ownedBoats[d.me.Boat] = true
	var bestOwnedBoatScore float64
	bestOwnedBoatTier := 0
	for bID := range ownedBoats {
		for i := range d.defs.Boats {
			if d.defs.Boats[i].ID == bID {
				score := d.CalcBoatScore(&d.defs.Boats[i])
				if score > bestOwnedBoatScore {
					bestOwnedBoatScore = score
				}
				if d.defs.Boats[i].Tier > bestOwnedBoatTier {
					bestOwnedBoatTier = d.defs.Boats[i].Tier
				}
				break
			}
		}
	}
	for _, b := range d.defs.Boats {
		if !b.Retired && !ownedBoats[b.ID] && b.Cost > 0 && d.me.Coins >= int64(b.Cost) {
			score := d.CalcBoatScore(&b)
			if score > bestOwnedBoatScore*1.02 && b.Tier >= bestOwnedBoatTier {
				return true
			}
		}
	}

	// 4. Check if a superior helper recruit is affordable
	ownedHelpers := make(map[string]bool)
	for _, h := range d.me.Helpers {
		ownedHelpers[h] = true
	}
	if d.me.Helper != "" {
		ownedHelpers[d.me.Helper] = true
	}
	var bestOwnedHelperScore float64
	for hID := range ownedHelpers {
		for i := range d.defs.Helpers {
			if d.defs.Helpers[i].ID == hID {
				score := d.CalcHelperScore(&d.defs.Helpers[i])
				if score > bestOwnedHelperScore {
					bestOwnedHelperScore = score
				}
				break
			}
		}
	}
	for _, h := range d.defs.Helpers {
		if !ownedHelpers[h.ID] && h.Cost > 0 && d.me.Coins >= int64(h.Cost) {
			score := d.CalcHelperScore(&h)
			if score > bestOwnedHelperScore {
				return true
			}
		}
	}

	// 5. Check if a superior weapon can be crafted at Forge
	if d.policy.TargetWeapon != "" {
		targetID := d.policy.TargetWeapon
		alreadyOwned := false
		for _, it := range d.me.Items {
			if it.W == targetID {
				alreadyOwned = true
				break
			}
		}
		if !alreadyOwned {
			for _, w := range d.defs.Weapons {
				if w.ID == targetID && w.Cost > 0 && d.me.Coins >= int64(w.Cost) {
					return true
				}
			}
			// When targeting a specific weapon and haven't accumulated enough coins, do not return to harbor for intermediate weapons
			return false
		}
	}

	var bestOwnedWeaponScore float64
	bestOwnedWeaponTier := 0
	for _, it := range d.me.Items {
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == it.W {
				score := d.CalcWeaponScore(&d.defs.Weapons[i], &it)
				if score > bestOwnedWeaponScore {
					bestOwnedWeaponScore = score
				}
				if d.defs.Weapons[i].Tier > bestOwnedWeaponTier {
					bestOwnedWeaponTier = d.defs.Weapons[i].Tier
				}
				break
			}
		}
	}
	for _, w := range d.defs.Weapons {
		if !w.Retired && !w.Exclusive && w.Cost > 0 && d.me.Coins >= int64(w.Cost) {
			score := d.CalcWeaponScore(&w, nil)
			if score > bestOwnedWeaponScore*1.02 && w.Tier >= bestOwnedWeaponTier {
				return true
			}
		}
	}

	return false
}

// checkNeedsIslandRun checks if a daily quest requires an island challenge
func (d *ReefBlastDaemon) checkNeedsIslandRun() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.me == nil || d.me.Daily == nil || d.me.Level < 8 {
		return false
	}
	for _, q := range d.me.Daily.List {
		if q.ID == "island" && !q.Claimed && q.Prog < q.Target {
			if d.me.Entered != nil {
				if _, already := d.me.Entered["rush"]; already {
					return false
				}
			}
			return true
		}
	}
	return false
}

// STRICT GUARANTEE: Never buy a vessel hull weaker or equal to what we already own
func (d *ReefBlastDaemon) checkHullUpgrade() bool {
	if !d.policy.AllowBuying || d.me == nil {
		return false
	}
	owned := make(map[string]bool)
	for _, b := range d.me.Boats {
		owned[b] = true
	}
	owned[d.me.Boat] = true

	var bestOwnedScore float64
	bestOwnedTier := 0

	d.defsLock.RLock()
	for bID := range owned {
		for i := range d.defs.Boats {
			if d.defs.Boats[i].ID == bID {
				score := d.CalcBoatScore(&d.defs.Boats[i])
				if score > bestOwnedScore {
					bestOwnedScore = score
				}
				if d.defs.Boats[i].Tier > bestOwnedTier {
					bestOwnedTier = d.defs.Boats[i].Tier
				}
				break
			}
		}
	}

	var affordable []BoatDef
	for _, b := range d.defs.Boats {
		if !b.Retired && !owned[b.ID] && b.Cost > 0 && d.me.Coins >= int64(b.Cost) {
			affordable = append(affordable, b)
		}
	}
	sort.Slice(affordable, func(i, j int) bool {
		return d.CalcBoatScore(&affordable[i]) > d.CalcBoatScore(&affordable[j])
	})

	if len(affordable) > 0 {
		cand := affordable[0]
		candScore := d.CalcBoatScore(&cand)
		if candScore > bestOwnedScore*1.02 && cand.Tier >= bestOwnedTier {
			d.defsLock.RUnlock()
			d.Log("UPGRADE", "Purchasing superior vessel hull: %s (Tier %d, Score %.0f vs Best Owned: %.0f) for %d coins!", cand.Name, cand.Tier, candScore, bestOwnedScore, cand.Cost)
			d.SailTo(26, -6.5, 3.5, 6*time.Second)
			// Human dwell pause at Shipyard dock (inspecting hull)
			time.Sleep(time.Duration(1200+int(d.personaSeed)%1000) * time.Millisecond)
			_ = d.Send(map[string]interface{}{"t": "buyBoat", "id": cand.ID})
			_ = d.Send(map[string]interface{}{"t": "equipBoat", "id": cand.ID})
			d.mu.Lock()
			d.me.Boat = cand.ID
			d.me.Boats = append(d.me.Boats, cand.ID)
			d.stats.UpgradesPurchased++
			d.mu.Unlock()
			return true
		}
	}
	d.defsLock.RUnlock()
	return false
}

// STRICT GUARANTEE: Never hire a helper weaker or equal to what we already own
func (d *ReefBlastDaemon) checkHelperRecruitment() bool {
	if !d.policy.AllowBuying || d.me == nil {
		return false
	}
	owned := make(map[string]bool)
	for _, h := range d.me.Helpers {
		owned[h] = true
	}
	if d.me.Helper != "" {
		owned[d.me.Helper] = true
	}

	var bestOwnedScore float64
	d.defsLock.RLock()
	for hID := range owned {
		for i := range d.defs.Helpers {
			if d.defs.Helpers[i].ID == hID {
				score := d.CalcHelperScore(&d.defs.Helpers[i])
				if score > bestOwnedScore {
					bestOwnedScore = score
				}
				break
			}
		}
	}

	var affordable []HelperDef
	for _, h := range d.defs.Helpers {
		if !owned[h.ID] && h.Cost > 0 && d.me.Coins >= int64(h.Cost) {
			affordable = append(affordable, h)
		}
	}
	sort.Slice(affordable, func(i, j int) bool {
		return d.CalcHelperScore(&affordable[i]) > d.CalcHelperScore(&affordable[j])
	})

	if len(affordable) > 0 {
		cand := affordable[0]
		candScore := d.CalcHelperScore(&cand)
		if candScore > bestOwnedScore {
			d.defsLock.RUnlock()
			d.Log("TAVERN", "Hiring superior crew helper: %s (%s, Score: %.0f vs Best Owned: %.0f) for %d coins!", cand.Name, cand.Desc, candScore, bestOwnedScore, cand.Cost)
			d.SailTo(-9, -1.0, 3.5, 6*time.Second)
			// Human dwell pause at Tavern dock (talking to recruit)
			time.Sleep(time.Duration(1200+int(d.personaSeed)%1000) * time.Millisecond)
			_ = d.Send(map[string]interface{}{"t": "buyHelper", "id": cand.ID})
			_ = d.Send(map[string]interface{}{"t": "equipHelper", "id": cand.ID})
			d.mu.Lock()
			d.me.Helper = cand.ID
			d.me.Helpers = append(d.me.Helpers, cand.ID)
			d.stats.UpgradesPurchased++
			d.mu.Unlock()
			return true
		}
	}
	d.defsLock.RUnlock()
	return false
}

// STRICT GUARANTEE: Never craft a weapon weaker or equal to what we already own
func (d *ReefBlastDaemon) checkForgeCrafting() bool {
	if !d.policy.AllowBuying || d.me == nil {
		return false
	}

	// 1. One-Off Target Weapon Exception (e.g. "x_plague" Plague Storm)
	if d.policy.TargetWeapon != "" {
		targetID := d.policy.TargetWeapon
		alreadyOwned := false
		for _, it := range d.me.Items {
			if it.W == targetID {
				alreadyOwned = true
				break
			}
		}
		if alreadyOwned {
			// Target achieved! Clear so this exception will not be repeated
			d.mu.Lock()
			d.policy.TargetWeapon = ""
			d.savePolicyLocked()
			d.mu.Unlock()
			return false
		}

		d.defsLock.RLock()
		var targetDef *WeaponDef
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == targetID {
				targetDef = &d.defs.Weapons[i]
				break
			}
		}
		d.defsLock.RUnlock()

		if targetDef != nil {
			if d.me.Coins >= int64(targetDef.Cost) {
				d.Log("FORGE", "🎯 ONE-OFF TARGET CRAFT: Forging %s (%s, Tier %d, %d coins)!", targetDef.Name, targetDef.ID, targetDef.Tier, targetDef.Cost)
				d.SailTo(-26, -6.5, 3.5, 6*time.Second)
				time.Sleep(time.Duration(1200+int(d.personaSeed)%1000) * time.Millisecond)
				_ = d.Send(map[string]interface{}{"t": "craft", "id": targetDef.ID, "n": 1})
				d.stats.UpgradesPurchased++
				// Target exception fulfilled! Clear so it won't be repeated
				d.mu.Lock()
				d.policy.TargetWeapon = ""
				d.savePolicyLocked()
				d.mu.Unlock()
				return true
			} else {
				// Specifically hoarding coins for target weapon:
				// Do NOT spend coins on any intermediate weapons!
				return false
			}
		}
	}

	var bestOwnedScore float64
	bestOwnedTier := 0
	d.defsLock.RLock()
	for _, it := range d.me.Items {
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == it.W {
				score := d.CalcWeaponScore(&d.defs.Weapons[i], &it)
				if score > bestOwnedScore {
					bestOwnedScore = score
				}
				if d.defs.Weapons[i].Tier > bestOwnedTier {
					bestOwnedTier = d.defs.Weapons[i].Tier
				}
				break
			}
		}
	}

	var craftable []WeaponDef
	for _, w := range d.defs.Weapons {
		if !w.Retired && !w.Exclusive && w.Cost > 0 && d.me.Coins >= int64(w.Cost) {
			craftable = append(craftable, w)
		}
	}
	sort.Slice(craftable, func(i, j int) bool {
		return d.CalcWeaponScore(&craftable[i], nil) > d.CalcWeaponScore(&craftable[j], nil)
	})

	if len(craftable) > 0 {
		cand := craftable[0]
		candScore := d.CalcWeaponScore(&cand, nil)
		if candScore > bestOwnedScore*1.02 && cand.Tier >= bestOwnedTier {
			d.defsLock.RUnlock()
			d.Log("FORGE", "Crafting superior weapon at Forge: %s (Tier %d, Score: %.0f vs Best Owned: %.0f) for %d coins!", cand.Name, cand.Tier, candScore, bestOwnedScore, cand.Cost)
			d.SailTo(-26, -6.5, 3.5, 6*time.Second)
			// Human dwell pause at Forge dock (crafting weapon)
			time.Sleep(time.Duration(1200+int(d.personaSeed)%1000) * time.Millisecond)
			_ = d.Send(map[string]interface{}{"t": "craft", "id": cand.ID, "n": 1})
			d.stats.UpgradesPurchased++
			return true
		}
	}
	d.defsLock.RUnlock()
	return false
}

// STRICT GUARANTEE: Never snipe a market deal weaker or equal to what we already own
func (d *ReefBlastDaemon) handleMarketList(listings []MarketListing) {
	if !d.policy.AllowBuying || d.me == nil || len(listings) == 0 {
		return
	}

	// Target Weapon Exception for Market:
	if d.policy.TargetWeapon != "" {
		targetID := d.policy.TargetWeapon
		alreadyOwned := false
		for _, it := range d.me.Items {
			if it.W == targetID {
				alreadyOwned = true
				break
			}
		}
		if alreadyOwned {
			d.mu.Lock()
			d.policy.TargetWeapon = ""
			d.savePolicyLocked()
			d.mu.Unlock()
		} else {
			for i := range listings {
				l := &listings[i]
				if l.Seller == d.me.Name || d.me.Coins < l.Price {
					continue
				}
				if l.Item.W == targetID {
					d.Log("MARKET", "🎯 ONE-OFF TARGET SNIPE: Sniping %s for %d coins!", targetID, l.Price)
					_ = d.Send(map[string]interface{}{"t": "buyMarket", "id": l.ID})
					d.stats.MarketDealsSniped++
					d.mu.Lock()
					d.policy.TargetWeapon = ""
					d.savePolicyLocked()
					d.mu.Unlock()
					return
				}
			}
			// When hoarding for target weapon, do not buy any other weapon on market
			return
		}
	}

	var bestOwnedScore float64
	bestOwnedTier := 0
	d.defsLock.RLock()
	for _, it := range d.me.Items {
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == it.W {
				score := d.CalcWeaponScore(&d.defs.Weapons[i], &it)
				if score > bestOwnedScore {
					bestOwnedScore = score
				}
				if d.defs.Weapons[i].Tier > bestOwnedTier {
					bestOwnedTier = d.defs.Weapons[i].Tier
				}
				break
			}
		}
	}

	var bestListing *MarketListing
	var bestDealScore float64 = -1
	var bestWDef *WeaponDef

	for i := range listings {
		l := &listings[i]
		if l.Seller == d.me.Name || d.me.Coins < l.Price {
			continue
		}
		for j := range d.defs.Weapons {
			if d.defs.Weapons[j].ID == l.Item.W {
				wDef := &d.defs.Weapons[j]
				score := d.CalcWeaponScore(wDef, &l.Item)
				if score > bestOwnedScore*1.02 && wDef.Tier >= bestOwnedTier && score > bestDealScore {
					bestDealScore = score
					bestListing = l
					bestWDef = wDef
				}
				break
			}
		}
	}

	if bestListing != nil && bestWDef != nil {
		d.defsLock.RUnlock()
		d.Log("MARKET", "🎯 SNIPING MARKET DEAL: Found superior %s (Tier %d, Score: %.0f vs Best Owned: %.0f) for %d coins!", bestWDef.Name, bestWDef.Tier, bestDealScore, bestOwnedScore, bestListing.Price)
		d.SailTo(9, -1.0, 3.5, 6*time.Second)
		time.Sleep(time.Duration(1200+int(d.personaSeed)%1000) * time.Millisecond)
		_ = d.Send(map[string]interface{}{"t": "mkBuy", "id": bestListing.ID})
		d.marketSniped = true
		d.stats.MarketDealsSniped++
		d.stats.UpgradesPurchased++
		return
	}
	d.defsLock.RUnlock()
}

func (d *ReefBlastDaemon) checkMarketSelling() bool {
	if !d.policy.AllowMarketSelling || d.me == nil || len(d.me.Items) <= 2 {
		return false
	}

	bestUID := d.me.Equipped
	var bestScore float64 = -1
	d.defsLock.RLock()
	for _, it := range d.me.Items {
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == it.W {
				score := d.CalcWeaponScore(&d.defs.Weapons[i], &it)
				if score > bestScore {
					bestScore = score
					bestUID = it.UID
				}
				break
			}
		}
	}

	for _, it := range d.me.Items {
		if it.UID == d.me.Equipped || it.UID == bestUID || it.Bound || it.Lock {
			continue
		}
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == it.W {
				w := &d.defs.Weapons[i]
				baseCost := float64(w.Cost)
				if baseCost <= 0 {
					baseCost = float64(w.Tier * 20000)
				}
				mult := 1.0 + it.Atk + it.Aspd + it.Crit
				asking := int64(math.Max(1000.0, baseCost*0.85*mult))

				d.defsLock.RUnlock()
				d.Log("MARKET", "Listing surplus blaster for sale: %s (Tier %d, UID: %s) for %d coins...", w.Name, w.Tier, it.UID, asking)
				d.SailTo(9, -1.0, 3.5, 6*time.Second)
				// Human dwell pause at Market dock (listing price)
				time.Sleep(time.Duration(1400+int(d.personaSeed)%1000) * time.Millisecond)
				_ = d.Send(map[string]interface{}{"t": "mkSell", "id": it.UID, "price": asking})
				return true
			}
		}
	}
	d.defsLock.RUnlock()
	return false
}

func (d *ReefBlastDaemon) autoSalvageJunk() {
	if !d.policy.AllowSalvaging || d.me == nil || len(d.me.Items) <= 4 {
		return
	}

	var bestScore float64
	d.defsLock.RLock()
	for _, it := range d.me.Items {
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == it.W {
				score := d.CalcWeaponScore(&d.defs.Weapons[i], &it)
				if score > bestScore {
					bestScore = score
				}
				break
			}
		}
	}

	var junkUIDs []string
	for _, it := range d.me.Items {
		if it.UID == d.me.Equipped || it.Bound || it.Lock {
			continue
		}
		for i := range d.defs.Weapons {
			if d.defs.Weapons[i].ID == it.W {
				if d.defs.Weapons[i].Tier >= 6 || d.defs.Weapons[i].Exclusive {
					continue
				}
				score := d.CalcWeaponScore(&d.defs.Weapons[i], &it)
				if score < bestScore*0.65 {
					junkUIDs = append(junkUIDs, it.UID)
				}
				break
			}
		}
	}
	d.defsLock.RUnlock()

	if len(junkUIDs) > 0 {
		d.Log("SALVAGE", "Auto-salvaging %d obsolete weapon(s) for scrap coins.", len(junkUIDs))
		_ = d.Send(map[string]interface{}{"t": "salvage", "uids": junkUIDs})
	}
}

func (d *ReefBlastDaemon) checkNeedsRespecLocked() bool {
	if d.hasAutoRespecced {
		return false
	}
	if d.me == nil || d.me.Skills == nil {
		return false
	}
	// Never respec if points are already unallocated and ready to spend
	if d.me.Points > 0 {
		return false
	}
	// Never attempt respec if player cannot afford respec fee
	if d.me.RespecCost > 0 && d.me.Coins < d.me.RespecCost {
		return false
	}
	hp := d.me.Skills["hp"]
	atk := d.me.Skills["atk"]
	aspd := d.me.Skills["aspd"]
	crit := d.me.Skills["crit"]

	// If player is level 20+ and has over 5 points dumped into HP,
	// BUT only if primary offensive stats are not yet maxed!
	// (At level > 155, atk/aspd/crit are capped at 50, so leftover points naturally overflow into def and hp)
	if d.me.Level >= 20 && hp > 5 && (atk < 50 || aspd < 50 || crit < 50) {
		return true
	}
	// If level 30+ but never allocated ATK
	if d.me.Level >= 30 && atk == 0 {
		return true
	}
	return false
}

func (d *ReefBlastDaemon) autoAllocateSkills() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.me == nil || d.me.Points <= 0 {
		return
	}

	maxPerSkill := 50
	if d.defs != nil && d.defs.SkillMax > 0 {
		maxPerSkill = d.defs.SkillMax
	}

	// Detect if current build has lifesteal weapon or high-tier vessel
	hasLeech := false
	if d.defs != nil {
		d.defsLock.RLock()
		for _, it := range d.me.Items {
			if it.UID == d.me.Equipped {
				for _, w := range d.defs.Weapons {
					if w.ID == it.W {
						if w.Leech > 0 {
							hasLeech = true
						}
						for _, tr := range w.Traits {
							if tr == "lifesteal" {
								hasLeech = true
							}
						}
						break
					}
				}
				break
			}
		}
		d.defsLock.RUnlock()
	}

	// Dynamic weight allocation:
	// With lifesteal weapons (1.2% hull repaired per hit) and high-tier armor (>100k HP, >300 DEF),
	// offense (ATK, ASPD, CRIT) directly multiplies clear speed AND healing rate!
	// HP is near-useless because leech heals a % of max HP and base hull is already huge.
	ratios := map[string]float64{
		"atk": 0.38, "aspd": 0.36, "crit": 0.18, "def": 0.04, "regen": 0.03, "hp": 0.01,
	}
	if !hasLeech && d.me.Level < 30 {
		ratios = map[string]float64{
			"atk": 0.32, "aspd": 0.28, "crit": 0.15, "def": 0.12, "hp": 0.10, "regen": 0.03,
		}
	}
	regenCap := 5

	if d.me.Skills == nil {
		d.me.Skills = make(map[string]int)
	}

	toSpendMap := make(map[string]int)

	for d.me.Points > 0 {
		bestStat := ""
		lowestScore := math.Inf(1)

		for stat, weight := range ratios {
			curVal := d.me.Skills[stat] + toSpendMap[stat]
			cap := maxPerSkill
			if stat == "regen" {
				cap = regenCap
			}
			if curVal < cap {
				score := (float64(curVal) + 0.5) / weight
				if score < lowestScore {
					lowestScore = score
					bestStat = stat
				}
			}
		}

		if bestStat == "" {
			break
		}

		toSpendMap[bestStat]++
		d.me.Points--
	}

	for stat, count := range toSpendMap {
		if count > 0 {
			_ = d.Send(map[string]interface{}{"t": "skill", "id": stat, "n": count})
			d.me.Skills[stat] += count
			d.Log("SKILL", "Allocated %d skill point(s) to %s (%d/%d)", count, strings.ToUpper(stat), d.me.Skills[stat], maxPerSkill)
		}
	}
}

func (d *ReefBlastDaemon) claimDailyQuests() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.me == nil || d.me.Daily == nil {
		return
	}

	for _, q := range d.me.Daily.List {
		if q.Prog >= q.Target && !q.Claimed {
			d.Log("QUEST", "Claiming daily quest: '%s' (+%d coins, +%d XP)", q.Text, q.Coins, q.XP)
			_ = d.Send(map[string]interface{}{"t": "questClaim", "id": q.ID})
		}
	}
	if !d.me.Daily.Bonus {
		allDone := true
		for _, q := range d.me.Daily.List {
			if !q.Claimed && q.Prog < q.Target {
				allDone = false
				break
			}
		}
		if allDone {
			_ = d.Send(map[string]interface{}{"t": "questClaim", "id": "bonus"})
		}
	}
}

func (d *ReefBlastDaemon) statusDumperLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	ticks := 0
	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			ticks++
			d.DumpStatus()
			// Reclaim operating system pages every 60 seconds
			if ticks%12 == 0 {
				debug.FreeOSMemory()
			}
		}
	}
}

func (d *ReefBlastDaemon) GetStatus() StatusDump {
	d.mu.Lock()
	defer d.mu.Unlock()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	rssMB := d.getProcessRSS()
	heapMB := float64(mem.HeapInuse) / 1024.0 / 1024.0

	zoneStr := "Harbor"
	if d.zone != nil {
		zoneStr = fmt.Sprintf("Zone %d", *d.zone)
	}

	if d.me != nil {
		for i := range d.me.Items {
			d.me.Items[i].Quality = d.CalcItemQuality(&d.me.Items[i])
		}
	}

	return StatusDump{
		UpdatedAt:    time.Now().Format(time.RFC3339),
		Engine:       "Reefblast Autonomous Go Daemon",
		MemoryRssMB:  math.Round(rssMB*10) / 10,
		MemoryHeapMB: math.Round(heapMB*10) / 10,
		Policy:       d.policy,
		Pos:          d.pos,
		Me:           d.me,
		Zone:         zoneStr,
		Combo:        d.combo,
		InParty:      d.inParty,
		PartyLeader:  d.partyLeader,
		BotRunning:   d.connected && !d.stopped,
		Stats:        d.stats,
	}
}

func (d *ReefBlastDaemon) DumpStatus() {
	status := d.GetStatus()
	data, err := json.MarshalIndent(status, "", "  ")
	if err == nil {
		_ = os.WriteFile(d.statusPath, data, 0644)
	}
}

func (d *ReefBlastDaemon) getProcessRSS() float64 {
	statmData, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 3.5
	}
	fields := strings.Fields(string(statmData))
	if len(fields) >= 2 {
		pages, _ := strconv.ParseInt(fields[1], 10, 64)
		return float64(pages*4096) / 1024.0 / 1024.0
	}
	return 3.5
}
