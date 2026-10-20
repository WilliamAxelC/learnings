package main

import (
	"encoding/json"
	"time"
)

// WeaponDef represents weapon configuration from defs.weapons
type WeaponDef struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Tier      int      `json:"tier"`
	Cost      int      `json:"cost"`
	DPS       float64  `json:"dps"`
	Damage    float64  `json:"damage"`
	FireRate  float64  `json:"fireRate"`
	Speed     float64  `json:"speed"`
	PL        int      `json:"pl"`
	Retired   bool     `json:"retired"`
	Exclusive bool     `json:"exclusive"`
	Scales    bool     `json:"scales"`
	Kind      string   `json:"kind"`
	Traits    []string `json:"traits"`
	Leech     float64  `json:"leech,omitempty"`
}

// BoatDef represents boat hull configuration from defs.boats
type BoatDef struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Tier      int                `json:"tier"`
	Cost      int                `json:"cost"`
	HP        float64            `json:"hp"`
	Def       float64            `json:"def"`
	Speed     float64            `json:"speed"`
	PL        int                `json:"pl"`
	Retired   bool               `json:"retired"`
	Exclusive bool               `json:"exclusive"`
	Buffs     map[string]float64 `json:"buffs"`
}

// HelperDef represents crew recruit from defs.helpers
type HelperDef struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Cost     int     `json:"cost"`
	Damage   float64 `json:"damage"`
	FireRate float64 `json:"fireRate"`
	DPS      float64 `json:"dps"`
	PL       int     `json:"pl"`
	Desc     string  `json:"desc"`
}

// ZoneDef represents fishing zone from defs.zones
type ZoneDef struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// HarborArea represents an interactable area dock in harbor
type HarborArea struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Z    float64 `json:"z"`
	R    float64 `json:"r"`
}

// HarborIsland represents a collision obstacle in harbor
type HarborIsland struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
	R float64 `json:"r"`
}

// HarborGate represents gate coordinates
type HarborGate struct {
	Zone int     `json:"zone"`
	X    float64 `json:"x"`
	Z    float64 `json:"z"`
	R    float64 `json:"r"`
}

// HarborDef represents harbor section
type HarborDef struct {
	Areas   []HarborArea   `json:"areas"`
	Gates   []HarborGate   `json:"gates"`
	Islands []HarborIsland `json:"islands"`
	Radius  float64        `json:"radius"`
	Spawn   []float64      `json:"spawn"`
}

// FishDef represents fish definition
type FishDef struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	HP     float64 `json:"hp"`
	Speed  float64 `json:"speed"`
	Score  float64 `json:"score"`
	Coins  float64 `json:"coins"`
	XP     float64 `json:"xp"`
	Boss   bool    `json:"boss"`
	Mini   bool    `json:"mini"`
}

// SkillDef represents skill entry in defs.skills
type SkillDef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RollDef represents stat roll range in defs.rolls
type RollDef struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

// GameDefs holds raw game definitions
type GameDefs struct {
	MaxLevel      int                    `json:"maxLevel"`
	SkillMax      int                    `json:"skillMax"`
	Weapons       []WeaponDef            `json:"weapons"`
	Boats         []BoatDef              `json:"boats"`
	Helpers       []HelperDef            `json:"helpers"`
	Zones         []ZoneDef              `json:"zones"`
	Fish          []FishDef              `json:"fish"`
	Skills        []SkillDef             `json:"skills"`
	Harbor        HarborDef              `json:"harbor"`
	TierDPS       []float64              `json:"tierDps"`
	ExclusiveEdge float64                `json:"exclusiveEdge"`
	ZonePower     []float64              `json:"zonePower"`
	Rolls         []RollDef              `json:"rolls"`
	Raw           map[string]interface{} `json:"-"`
}

// Item represents an owned weapon
type Item struct {
	UID     string  `json:"uid"`
	W       string  `json:"w"`
	Atk     float64 `json:"atk"`
	Aspd    float64 `json:"aspd"`
	Crit    float64 `json:"crit"`
	Bound   bool    `json:"bound"`
	Lock    bool    `json:"lock"`
	By      string  `json:"by,omitempty"`
	Quality int     `json:"quality,omitempty"`
}

// Quest represents a daily quest
type Quest struct {
	ID      string  `json:"id"`
	Text    string  `json:"text"`
	Prog    float64 `json:"prog"`
	Target  float64 `json:"target"`
	Claimed bool    `json:"claimed"`
	Coins   int     `json:"coins"`
	XP      int     `json:"xp"`
}

// DailyQuests represents daily quest list
type DailyQuests struct {
	Bonus bool    `json:"bonus"`
	List  []Quest `json:"list"`
}

// PlayerMe represents player state
type PlayerMe struct {
	Name     string             `json:"name"`
	Level    int                `json:"level"`
	Coins    int64              `json:"coins"`
	XP       float64            `json:"xp"`
	NextXP   float64            `json:"nextXP,omitempty"`
	Boat     string             `json:"boat"`
	Boats    []string           `json:"boats"`
	Helper   string             `json:"helper"`
	Helpers  []string           `json:"helpers"`
	Points   int                `json:"points"`
	Skills   map[string]int     `json:"skills"`
	RespecCost int64            `json:"respecCost,omitempty"`
	Equipped string             `json:"equipped"`
	Items    []Item             `json:"items"`
	Daily    *DailyQuests       `json:"daily,omitempty"`
	Entered  map[string]interface{} `json:"entered,omitempty"`
}

// RosterPlayer tracks other players and our own boat X position in room
type RosterPlayer struct {
	Slot   int     `json:"slot"`
	Name   string  `json:"name"`
	X      float64 `json:"x"`
	Boat   string  `json:"boat"`
	Char   string  `json:"char"`
	Helper string  `json:"helper"`
}

// Position tracks coordinates
type Position struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
	R float64 `json:"r"`
}

// FishState tracks fish tracking in zone
type FishState struct {
	ID          int64   `json:"id"`
	Ty          int     `json:"ty"`
	X           float64 `json:"x"`
	Z           float64 `json:"z"`
	TX          float64 `json:"tx"`
	TZ          float64 `json:"tz"`
	VX          float64 `json:"vx"`
	VZ          float64 `json:"vz"`
	HP          float64 `json:"hp"`
	Flags       int     `json:"flags"`
	Dir         float64 `json:"dir"`
	LastT       int64   `json:"lastT"`
	LastSeenGen uint64  `json:"-"`
}

// ShotState tracks enemy projectile
type ShotState struct {
	ID          int64   `json:"id"`
	X           float64 `json:"x"`
	Z           float64 `json:"z"`
	TX          float64 `json:"tx"`
	TZ          float64 `json:"tz"`
	VX          float64 `json:"vx"`
	VZ          float64 `json:"vz"`
	Kind        string  `json:"kind"`
	HP          float64 `json:"hp"`
	LastT       int64   `json:"lastT"`
	LastSeenGen uint64  `json:"-"`
}

// Policy reflects policy.json
type Policy struct {
	AllowBuying        bool      `json:"allowBuying"`
	AllowSalvaging     bool      `json:"allowSalvaging"`
	AllowMarketSelling bool      `json:"allowMarketSelling"`
	TargetWeapon       string    `json:"targetWeapon,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// Stats tracks session performance
type Stats struct {
	StartedAt          int64   `json:"startedAt"`
	StartCoins         int64   `json:"startCoins"`
	StartLevel         int     `json:"startLevel"`
	StartXp            float64 `json:"startXp"`
	BestCombo          int     `json:"bestCombo"`
	KillsThisSession   int     `json:"killsThisSession"`
	ShotsIntercepted   int     `json:"shotsIntercepted"`
	UpgradesPurchased  int     `json:"upgradesPurchased"`
	MarketDealsSniped  int     `json:"marketDealsSniped"`
}

// MarketListing represents player market item
type MarketListing struct {
	ID     int64  `json:"id"`
	Seller string `json:"seller"`
	Price  int64  `json:"price"`
	Item   Item   `json:"item"`
}

// StatusDump matches logs/status.json for Discord bot
type StatusDump struct {
	UpdatedAt     string                 `json:"updatedAt"`
	Engine        string                 `json:"engine"`
	MemoryRssMB   float64                `json:"memoryRssMb"`
	MemoryHeapMB  float64                `json:"memoryHeapMb"`
	Policy        Policy                 `json:"policy"`
	DriftDetected map[string]interface{} `json:"driftDetected"`
	Pos           Position               `json:"pos"`
	Me            *PlayerMe              `json:"me"`
	Zone          string                 `json:"zone"`
	Combo         int                    `json:"combo"`
	InParty       bool                   `json:"inParty"`
	PartyLeader   string                 `json:"partyLeader,omitempty"`
	BotRunning    bool                   `json:"botRunning"`
	Stats         Stats                  `json:"stats"`
}

// IncomingMessage is generic WebSocket payload
type IncomingMessage struct {
	T string `json:"t"`

	// hello / me
	Me *PlayerMe `json:"me,omitempty"`

	// hello / left
	Pos    []float64 `json:"pos,omitempty"`
	Reason string    `json:"reason,omitempty"`

	// pos
	X float64 `json:"x,omitempty"`
	Z float64 `json:"z,omitempty"`

	// joined
	Zone  int         `json:"zone,omitempty"`
	Slot  int         `json:"slot,omitempty"`
	Party interface{} `json:"party,omitempty"`
	Mode  string      `json:"mode,omitempty"`
	Tier  int         `json:"tier,omitempty"`

	// room
	Players []RosterPlayer `json:"players,omitempty"`

	// kicked
	Msg string `json:"msg,omitempty"`

	// invites
	From string `json:"from,omitempty"`

	// market
	Listings []MarketListing `json:"listings,omitempty"`

	// snapshot
	P  [][]interface{}   `json:"p,omitempty"`
	F  [][]interface{}   `json:"f,omitempty"`
	SH [][]interface{}   `json:"sh,omitempty"`
	EV []json.RawMessage `json:"ev,omitempty"`
}
