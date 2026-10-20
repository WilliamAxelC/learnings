package main

import (
	"math"
	"testing"
)

func TestOrganicWaypointsAvoidance(t *testing.T) {
	d := &ReefBlastDaemon{botName: "cuang-bot"}

	// Path from South dock to North gate (0, 180) passes through Island 0 (0, -32)
	wps := d.getWaypoints(0, -60, 0, 10, 42.0)
	if len(wps) != 2 {
		t.Fatalf("Expected 2 waypoints for island avoidance, got %d", len(wps))
	}

	avoidanceWP := wps[0]
	// Distance to center of Island 0 (0, -32) must be >= 25.0
	dist := math.Hypot(avoidanceWP[0]-0.0, avoidanceWP[1]-(-32.0))
	if dist < 24.0 {
		t.Errorf("Avoidance waypoint too close to island center: dist=%.2f < 24.0", dist)
	}
}

func TestOrganicOpenWaterCurvedWaypoints(t *testing.T) {
	d := &ReefBlastDaemon{botName: "william"}

	// Long open-water voyage (> 38 units) away from Island 0
	wps := d.getWaypoints(9, -1, 0, 180, 88.0)
	if len(wps) != 2 {
		t.Fatalf("Expected 2 waypoints for long curved route, got %d", len(wps))
	}

	midWP := wps[0]
	// Must not be a flat straight midpoint (must have natural curve offset)
	straightMidX := (9.0 + 0.0) / 2.0
	if math.Abs(midWP[0]-straightMidX) < 0.05 {
		t.Errorf("Midpoint was not curved laterally: midWP.X=%.2f, straightMidX=%.2f", midWP[0], straightMidX)
	}
}

func TestOrganicPhysicsSpeedCap(t *testing.T) {
	d := &ReefBlastDaemon{botName: "test-bot"}
	dt := 0.05
	baseSpeed := 9.5

	// Simulate 100 ticks with multi-harmonic wave sway and throttle surging
	for tick := 0; tick < 100; tick++ {
		ix := 0.6
		iz := 0.8
		throttle := 1.02
		currSpeed := baseSpeed * throttle

		d.stepHarborPhysics(dt, ix, iz, currSpeed)
		currentSp := math.Hypot(d.vx, d.vz)

		if currentSp > baseSpeed*1.03 {
			t.Fatalf("Tick %d: Current speed %.2f exceeded allowed baseSpeed %.2f", tick, currentSp, baseSpeed)
		}
	}
}

func TestPersonaDeterminismAndDistribution(t *testing.T) {
	// Verify determinism: same bot gets same persona and seed
	d1 := NewReefBlastDaemon(nil, "cuang-bot", "pass", "tok")
	d2 := NewReefBlastDaemon(nil, "cuang-bot", "pass", "tok")
	if d1.persona != d2.persona || d1.personaSeed != d2.personaSeed {
		t.Fatalf("Persona must be deterministic: d1=%d, d2=%d", d1.persona, d2.persona)
	}

	// Verify fleet-wide distribution across 300 generated bot names
	counts := make(map[int]int)
	for i := 0; i < 300; i++ {
		name := "bot_" + string(rune('a'+(i%26))) + string(rune('0'+(i/26)))
		d := NewReefBlastDaemon(nil, name, "pass", "tok")
		counts[d.persona]++
	}

	// Expect healthy spread: Grinder (0) ~50-75%, Trader (1) ~10-30%, Explorer (2) ~5-25%
	grinderPct := float64(counts[0]) / 300.0
	traderPct := float64(counts[1]) / 300.0
	explorerPct := float64(counts[2]) / 300.0

	if grinderPct < 0.45 || grinderPct > 0.80 {
		t.Errorf("Grinder ratio out of expected bounds: %.2f", grinderPct)
	}
	if traderPct < 0.08 || traderPct > 0.35 {
		t.Errorf("Trader ratio out of expected bounds: %.2f", traderPct)
	}
	if explorerPct < 0.05 || explorerPct > 0.30 {
		t.Errorf("Explorer ratio out of expected bounds: %.2f", explorerPct)
	}
}

func TestCombatAimSmoothingAndTremor(t *testing.T) {
	d := NewReefBlastDaemon(nil, "william", "pass", "tok")

	// 1. Initial aim at (0, 0)
	d.pushAim(0, 0, true, false)
	initX := d.lastAimSent.x
	_ = d.lastAimSent.z

	// 2. Target suddenly shifts to (10, -5) (e.g. fish appears on right)
	// Normal smooth aim must not snap immediately to (10, -5) in 1 frame
	d.lastAimSent.t = 0 // allow packet send
	d.pushAim(10, -5, true, false)
	flickX := d.lastAimSent.x
	flickZ := d.lastAimSent.z

	// Must have progressed towards target without instant full 100% snap
	if math.Abs(flickX-10.0) < 0.1 && math.Abs(flickZ-(-5.0)) < 0.1 {
		t.Errorf("Aim snapped instantaneously instead of smoothing: flickX=%.2f, target=10.0", flickX)
	}
	if flickX <= initX {
		t.Errorf("Aim failed to move towards target: flickX=%.2f, initX=%.2f", flickX, initX)
	}

	// 3. Emergency snap for Doom Orb must be instant (emergency=true)
	d.lastAimSent.t = 0
	d.pushAim(-14.2, 2.5, true, true)
	if math.Abs(d.lastAimSent.x-(-14.2)) > 0.01 || math.Abs(d.lastAimSent.z-2.5) > 0.01 {
		t.Errorf("Emergency aim failed to snap instantly: x=%.2f (want -14.2), z=%.2f (want 2.5)", d.lastAimSent.x, d.lastAimSent.z)
	}
}

