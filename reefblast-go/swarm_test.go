package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSwarmManagerDeriveKey(t *testing.T) {
	if k := deriveBotKey("helmszack@cuang.dev"); k != "helmszack" {
		t.Errorf("Expected helmszack, got %s", k)
	}
	if k := deriveBotKey("capt_1a74cb@cuang.dev"); k != "capt_1a74cb" {
		t.Errorf("Expected capt_1a74cb, got %s", k)
	}
}

func TestSwarmManagerConfigSaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "swarm_test_*")
	if err != nil {
		t.Fatalf("Failed to create tmpDir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "fleet.json")
	sm := NewSwarmManager(nil, configPath, 9999)

	acc1 := &SwarmBotAccount{
		Key:      "bot1",
		Email:    "bot1@cuang.dev",
		Password: "pass1",
		Enabled:  true,
	}
	acc2 := &SwarmBotAccount{
		Key:      "bot2",
		Email:    "bot2@cuang.dev",
		Password: "pass2",
		Enabled:  false,
	}

	sm.mu.Lock()
	sm.accounts[acc1.Key] = acc1
	sm.accounts[acc2.Key] = acc2
	err = sm.SaveConfigLocked()
	sm.mu.Unlock()

	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("Config file does not exist: %v", err)
	}

	// Load into fresh manager
	sm2 := NewSwarmManager(nil, configPath, 9999)
	if err := sm2.LoadConfig(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if len(sm2.accounts) != 2 {
		t.Fatalf("Expected 2 accounts, got %d", len(sm2.accounts))
	}
	if sm2.accounts["bot1"].Email != "bot1@cuang.dev" || !sm2.accounts["bot1"].Enabled {
		t.Errorf("bot1 data mismatch")
	}
	if sm2.accounts["bot2"].Email != "bot2@cuang.dev" || sm2.accounts["bot2"].Enabled {
		t.Errorf("bot2 data mismatch")
	}
}

func TestSwarmManagerHTTPHealthAndFleet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "swarm_http_test_*")
	if err != nil {
		t.Fatalf("Failed to create tmpDir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "fleet.json")
	sm := NewSwarmManager(nil, configPath, 9999)
	sm.accounts["test_bot"] = &SwarmBotAccount{
		Key:      "test_bot",
		Email:    "test_bot@cuang.dev",
		Password: "pass",
		Enabled:  false,
	}

	// Test /health
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	sm.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("/health status = %d, want 200", rr.Code)
	}
	var healthRes map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &healthRes); err != nil {
		t.Fatalf("Failed to parse /health response: %v", err)
	}
	if healthRes["swarm"] != true || healthRes["total"].(float64) != 1 {
		t.Errorf("Health response mismatch: %v", healthRes)
	}

	// Test /fleet
	reqFleet := httptest.NewRequest("GET", "/fleet", nil)
	rrFleet := httptest.NewRecorder()
	sm.handleFleet(rrFleet, reqFleet)

	if rrFleet.Code != http.StatusOK {
		t.Errorf("/fleet status = %d, want 200", rrFleet.Code)
	}
	var fleetRes map[string]interface{}
	if err := json.Unmarshal(rrFleet.Body.Bytes(), &fleetRes); err != nil {
		t.Fatalf("Failed to parse /fleet response: %v", err)
	}
	bots := fleetRes["bots"].([]interface{})
	if len(bots) != 1 {
		t.Fatalf("Expected 1 bot in /fleet, got %d", len(bots))
	}
	bot0 := bots[0].(map[string]interface{})
	if bot0["key"] != "test_bot" || bot0["zone"] != "Offline" {
		t.Errorf("Bot summary mismatch: %v", bot0)
	}
}

func TestTargetWeaponOneOffException(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_target_weapon_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	defs := &GameDefs{
		Weapons: []WeaponDef{
			{ID: "krakenthirst", Name: "Kraken's Thirst", Tier: 6, DPS: 12040, Cost: 7590000},
			{ID: "x_hexcoil", Name: "Hexfire Coil", Tier: 7, DPS: 12839, Cost: 29700000},
			{ID: "x_plague", Name: "Plague Storm", Tier: 7, DPS: 13846, Cost: 37300000},
		},
	}

	policyPath := filepath.Join(tmpDir, "policy.json")
	initialPolicy := Policy{
		AllowBuying:  true,
		TargetWeapon: "x_plague",
	}
	pData, _ := json.Marshal(initialPolicy)
	_ = os.WriteFile(policyPath, pData, 0644)

	d := NewReefBlastDaemonWithPaths(defs, "william", "pass", "tok", tmpDir, filepath.Join(tmpDir, "status.json"), filepath.Join(tmpDir, "bot.log"))
	d.me = &PlayerMe{
		Name:  "william",
		Coins: 29719288, // Has enough for Hexfire Coil (29.7M), but NOT enough for Plague Storm (37.3M)
		Items: []Item{
			{UID: "uid1", W: "krakenthirst", Atk: 0.14, Aspd: 0.15},
		},
	}

	// 1. With coins = 29.7M and TargetWeapon = "x_plague":
	// Must NOT craft Hexfire Coil, even though it's affordable!
	crafted := d.checkForgeCrafting()
	if crafted {
		t.Errorf("Expected checkForgeCrafting to return false while hoarding for Plague Storm, got true")
	}

	// 2. Now William farms and reaches 37.5M coins:
	d.me.Coins = 37500000
	craftedTarget := d.checkForgeCrafting()
	if !craftedTarget {
		t.Errorf("Expected checkForgeCrafting to craft target weapon Plague Storm at 37.5M coins, got false")
	}

	// 3. Verify TargetWeapon is now CLEARED so this exception won't be repeated:
	if d.policy.TargetWeapon != "" {
		t.Errorf("Expected TargetWeapon to be cleared after craft, got '%s'", d.policy.TargetWeapon)
	}

	// Check persisted policy.json
	d.readPolicy()
	if d.policy.TargetWeapon != "" {
		t.Errorf("Expected TargetWeapon in policy.json to be cleared after craft, got '%s'", d.policy.TargetWeapon)
	}
}

func TestCalcItemQuality(t *testing.T) {
	defs := &GameDefs{
		Rolls: []RollDef{
			{ID: "atk", Name: "Bonus ATK", Min: -0.2, Max: 0.5},
			{ID: "aspd", Name: "Bonus ATK speed", Min: -0.2, Max: 0.5},
			{ID: "crit", Name: "Bonus crit rate", Min: -0.05, Max: 0.15},
		},
	}
	d := NewReefBlastDaemonWithPaths(defs, "testbot", "pass", "tok", "", "", "")

	// 1. Bound starter weapon
	starter := &Item{UID: "u1", W: "popper", Bound: true}
	if q := d.CalcItemQuality(starter); q != 0 {
		t.Errorf("Expected starter quality 0, got %d", q)
	}

	// 2. William's actual Plague Storm
	plague := &Item{UID: "u2", W: "x_plague", Atk: 0.0738, Aspd: 0.2039, Crit: 0.0912}
	if q := d.CalcItemQuality(plague); q != 56 {
		t.Errorf("Expected William's Plague Storm quality 56%%, got %d%%", q)
	}

	// 3. Perfect max roll
	godRoll := &Item{UID: "u3", W: "x_plague", Atk: 0.50, Aspd: 0.50, Crit: 0.15}
	if q := d.CalcItemQuality(godRoll); q != 100 {
		t.Errorf("Expected god roll quality 100%%, got %d%%", q)
	}

	// 4. Worst min roll
	trashRoll := &Item{UID: "u4", W: "x_plague", Atk: -0.20, Aspd: -0.20, Crit: -0.05}
	if q := d.CalcItemQuality(trashRoll); q != 0 {
		t.Errorf("Expected trash roll quality 0%%, got %d%%", q)
	}
}
