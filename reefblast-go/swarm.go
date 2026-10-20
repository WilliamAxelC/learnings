package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type SwarmBotAccount struct {
	Key      string `json:"key"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Token    string `json:"token,omitempty"`
	Enabled  bool   `json:"enabled"`
	Policy   Policy `json:"policy"`
}

type SwarmFleetFile struct {
	Accounts []*SwarmBotAccount `json:"accounts"`
}

type SwarmBotSummary struct {
	Key          string     `json:"key"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Running      bool       `json:"running"`
	Level        int        `json:"level"`
	Coins        int64      `json:"coins"`
	Zone         string     `json:"zone"`
	Combo        int        `json:"combo"`
	Boat         string     `json:"boat"`
	Helper       string     `json:"helper"`
	MemoryRssMB  float64    `json:"memoryRssMb"`
	MemoryHeapMB float64    `json:"memoryHeapMb"`
	Policy       Policy     `json:"policy"`
	Status       StatusDump `json:"status"`
}

type SwarmManager struct {
	mu         sync.RWMutex
	defs       *GameDefs
	configPath string
	logsDir    string
	accounts   map[string]*SwarmBotAccount
	daemons    map[string]*ReefBlastDaemon
	port       int
	server     *http.Server
}

func NewSwarmManager(defs *GameDefs, configPath string, port int) *SwarmManager {
	dir := filepath.Dir(configPath)
	logsDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logsDir, 0755)

	return &SwarmManager{
		defs:       defs,
		configPath: configPath,
		logsDir:    logsDir,
		accounts:   make(map[string]*SwarmBotAccount),
		daemons:    make(map[string]*ReefBlastDaemon),
		port:       port,
	}
}

func (sm *SwarmManager) LoadConfig() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	data, err := os.ReadFile(sm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var ff SwarmFleetFile
	if err := json.Unmarshal(data, &ff); err != nil {
		return err
	}

	for _, acc := range ff.Accounts {
		if acc.Key == "" {
			acc.Key = deriveBotKey(acc.Email)
		}
		sm.accounts[acc.Key] = acc
	}
	return nil
}

func (sm *SwarmManager) SaveConfigLocked() error {
	ff := SwarmFleetFile{
		Accounts: make([]*SwarmBotAccount, 0, len(sm.accounts)),
	}
	for _, acc := range sm.accounts {
		ff.Accounts = append(ff.Accounts, acc)
	}
	sort.Slice(ff.Accounts, func(i, j int) bool {
		return ff.Accounts[i].Key < ff.Accounts[j].Key
	})

	data, err := json.MarshalIndent(ff, "", "  ")
	if err != nil {
		return err
	}

	tmp := sm.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, sm.configPath)
}

func deriveBotKey(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return fmt.Sprintf("bot_%d", time.Now().UnixNano()%10000)
}

func (sm *SwarmManager) Start() error {
	if err := sm.LoadConfig(); err != nil {
		fmt.Printf("[WARN] Error loading fleet config %s: %v\n", sm.configPath, err)
	}

	sm.mu.Lock()
	fmt.Printf("[SWARM] Starting %d fleet accounts from config...\n", len(sm.accounts))
	for _, acc := range sm.accounts {
		if acc.Enabled {
			sm.startDaemonLocked(acc)
			time.Sleep(150 * time.Millisecond) // Staggered account launch
		}
	}
	sm.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", sm.handleHealth)
	mux.HandleFunc("/fleet", sm.handleFleet)
	mux.HandleFunc("/bot", sm.handleBot)
	mux.HandleFunc("/fleet/add", sm.handleAdd)
	mux.HandleFunc("/fleet/start", sm.handleStart)
	mux.HandleFunc("/fleet/stop", sm.handleStop)
	mux.HandleFunc("/fleet/policy", sm.handlePolicy)
	mux.HandleFunc("/fleet/remove", sm.handleDelete)

	sm.server = &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", sm.port),
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		fmt.Printf("[SWARM] Fleet HTTP API listening on http://0.0.0.0:%d\n", sm.port)
		if err := sm.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[SWARM ERROR] HTTP server failed: %v\n", err)
		}
	}()

	return nil
}

func (sm *SwarmManager) StopAll() {
	if sm.server != nil {
		_ = sm.server.Close()
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for key, d := range sm.daemons {
		fmt.Printf("[SWARM] Stopping daemon '%s'...\n", key)
		d.Stop()
	}
	sm.daemons = make(map[string]*ReefBlastDaemon)
}

func (sm *SwarmManager) startDaemonLocked(acc *SwarmBotAccount) {
	if _, running := sm.daemons[acc.Key]; running {
		return
	}

	statusPath := filepath.Join(sm.logsDir, fmt.Sprintf("status_%s.json", acc.Key))
	logPath := filepath.Join(sm.logsDir, fmt.Sprintf("auto_progression_%s.log", acc.Key))

	d := NewReefBlastDaemonWithPaths(
		sm.defs,
		acc.Email,
		acc.Password,
		acc.Token,
		filepath.Dir(sm.configPath),
		statusPath,
		logPath,
	)
	if acc.Policy.UpdatedAt.IsZero() {
		acc.Policy = Policy{
			AllowBuying:        true,
			AllowSalvaging:     false,
			AllowMarketSelling: false,
			UpdatedAt:          time.Now(),
		}
	}
	d.mu.Lock()
	d.policy = acc.Policy
	d.mu.Unlock()

	sm.daemons[acc.Key] = d

	go func(key string, daemon *ReefBlastDaemon) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[SWARM RECOVER] Bot '%s' crashed with panic: %v. Restarting in 5s...\n", key, r)
				time.Sleep(5 * time.Second)
				sm.mu.Lock()
				if a, ok := sm.accounts[key]; ok && a.Enabled {
					delete(sm.daemons, key)
					sm.startDaemonLocked(a)
				}
				sm.mu.Unlock()
			}
		}()
		daemon.Start()
	}(acc.Key, d)
}

func (sm *SwarmManager) stopDaemonLocked(key string) {
	if d, ok := sm.daemons[key]; ok {
		d.Stop()
		delete(sm.daemons, key)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (sm *SwarmManager) handleHealth(w http.ResponseWriter, r *http.Request) {
	sm.mu.RLock()
	total := len(sm.accounts)
	running := len(sm.daemons)
	sm.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"version": "2.5.0",
		"swarm":   true,
		"total":   total,
		"running": running,
	})
}

func (sm *SwarmManager) handleFleet(w http.ResponseWriter, r *http.Request) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	keys := make([]string, 0, len(sm.accounts))
	for k := range sm.accounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	summaries := make([]SwarmBotSummary, 0, len(keys))
	for _, k := range keys {
		acc := sm.accounts[k]
		d := sm.daemons[k]

		var dump StatusDump
		running := false
		if d != nil {
			dump = d.GetStatus()
			running = dump.BotRunning
		} else {
			name := acc.Key
			dump = StatusDump{
				UpdatedAt:   time.Now().Format(time.RFC3339),
				Engine:      "Reefblast Autonomous Go Daemon",
				Policy:      acc.Policy,
				Zone:        "Offline",
				BotRunning:  false,
				MemoryRssMB: 0,
				Me: &PlayerMe{
					Name:  name,
					Level: 1,
				},
			}
		}

		name := acc.Key
		level := 1
		var coins int64
		boat := "boat-fishing-small"
		helper := "dex"
		if dump.Me != nil {
			if dump.Me.Name != "" {
				name = dump.Me.Name
			}
			level = dump.Me.Level
			coins = dump.Me.Coins
			if dump.Me.Boat != "" {
				boat = dump.Me.Boat
			}
			if dump.Me.Helper != "" {
				helper = dump.Me.Helper
			}
		}

		summaries = append(summaries, SwarmBotSummary{
			Key:          k,
			Email:        acc.Email,
			Name:         name,
			Running:      running,
			Level:        level,
			Coins:        coins,
			Zone:         dump.Zone,
			Combo:        dump.Combo,
			Boat:         boat,
			Helper:       helper,
			MemoryRssMB:  dump.MemoryRssMB,
			MemoryHeapMB: dump.MemoryHeapMB,
			Policy:       acc.Policy,
			Status:       dump,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"bots": summaries,
	})
}

func (sm *SwarmManager) handleBot(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, `{"ok":false,"error":"missing key parameter"}`, http.StatusBadRequest)
		return
	}

	sm.mu.RLock()
	acc, exists := sm.accounts[key]
	d := sm.daemons[key]
	sm.mu.RUnlock()

	if !exists {
		http.Error(w, `{"ok":false,"error":"bot not found"}`, http.StatusNotFound)
		return
	}

	var dump StatusDump
	if d != nil {
		dump = d.GetStatus()
	} else {
		dump = StatusDump{
			UpdatedAt:  time.Now().Format(time.RFC3339),
			Engine:     "Reefblast Autonomous Go Daemon",
			Policy:     acc.Policy,
			Zone:       "Offline",
			BotRunning: false,
			Me: &PlayerMe{
				Name:  acc.Key,
				Level: 1,
			},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"key":    key,
		"email":  acc.Email,
		"status": dump,
	})
}

func (sm *SwarmManager) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"ok":false,"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"ok":false,"error":"could not read body"}`, http.StatusBadRequest)
		return
	}

	var single SwarmBotAccount
	var batch struct {
		Bots []SwarmBotAccount `json:"bots"`
	}

	var toAdd []SwarmBotAccount
	if err := json.Unmarshal(body, &batch); err == nil && len(batch.Bots) > 0 {
		toAdd = batch.Bots
	} else if err := json.Unmarshal(body, &single); err == nil && (single.Email != "" || single.Key != "") {
		toAdd = []SwarmBotAccount{single}
	} else {
		http.Error(w, `{"ok":false,"error":"invalid JSON payload"}`, http.StatusBadRequest)
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	addedCount := 0
	for _, b := range toAdd {
		if b.Key == "" {
			b.Key = deriveBotKey(b.Email)
		}
		b.Enabled = true
		if b.Policy.UpdatedAt.IsZero() {
			b.Policy = Policy{
				AllowBuying:        true,
				AllowSalvaging:     false,
				AllowMarketSelling: false,
				UpdatedAt:          time.Now(),
			}
		}

		acc := b
		sm.accounts[acc.Key] = &acc
		sm.startDaemonLocked(&acc)
		addedCount++
	}

	_ = sm.SaveConfigLocked()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"added": addedCount,
	})
}

func (sm *SwarmManager) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"ok":false,"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key  string   `json:"key"`
		Keys []string `json:"keys"`
		All  bool     `json:"all"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	targetKeys := make(map[string]bool)
	if req.All {
		for k := range sm.accounts {
			targetKeys[k] = true
		}
	} else {
		if req.Key != "" {
			targetKeys[req.Key] = true
		}
		for _, k := range req.Keys {
			targetKeys[k] = true
		}
	}

	started := 0
	for k := range targetKeys {
		if acc, ok := sm.accounts[k]; ok {
			acc.Enabled = true
			sm.startDaemonLocked(acc)
			started++
		}
	}

	_ = sm.SaveConfigLocked()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"started": started,
	})
}

func (sm *SwarmManager) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"ok":false,"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key  string   `json:"key"`
		Keys []string `json:"keys"`
		All  bool     `json:"all"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	targetKeys := make(map[string]bool)
	if req.All {
		for k := range sm.accounts {
			targetKeys[k] = true
		}
	} else {
		if req.Key != "" {
			targetKeys[req.Key] = true
		}
		for _, k := range req.Keys {
			targetKeys[k] = true
		}
	}

	stopped := 0
	for k := range targetKeys {
		if acc, ok := sm.accounts[k]; ok {
			acc.Enabled = false
			sm.stopDaemonLocked(k)
			stopped++
		}
	}

	_ = sm.SaveConfigLocked()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"stopped": stopped,
	})
}

func (sm *SwarmManager) handlePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"ok":false,"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key    string `json:"key"`
		Policy Policy `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		http.Error(w, `{"ok":false,"error":"invalid payload"}`, http.StatusBadRequest)
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc, ok := sm.accounts[req.Key]
	if !ok {
		http.Error(w, `{"ok":false,"error":"bot not found"}`, http.StatusNotFound)
		return
	}

	req.Policy.UpdatedAt = time.Now()
	acc.Policy = req.Policy

	if d, running := sm.daemons[req.Key]; running {
		d.mu.Lock()
		d.policy = req.Policy
		d.mu.Unlock()
	}

	_ = sm.SaveConfigLocked()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"key":    req.Key,
		"policy": req.Policy,
	})
}

func (sm *SwarmManager) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		var req struct {
			Key string `json:"key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		key = req.Key
	}

	if key == "" {
		http.Error(w, `{"ok":false,"error":"missing key"}`, http.StatusBadRequest)
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.accounts[key]; !ok {
		http.Error(w, `{"ok":false,"error":"bot not found"}`, http.StatusNotFound)
		return
	}

	sm.stopDaemonLocked(key)
	delete(sm.accounts, key)
	_ = sm.SaveConfigLocked()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"removed": key,
	})
}
