# Reefblast Autonomous Naval System — Agent Handoff & Architecture Guide

> **Target Audience**: Next Autonomous Agent / Core Developer  
> **System Version**: 2.4.0 (Go Native Engine + Discord Control Center)  
> **Status**: Production Stable · High-Performance AFK Farming Active

---

## 1. Executive Summary & Live State

The Reefblast botting fleet has been migrated from Node.js Puppeteer/WS clients to a **native, compiled Go micro-daemon** (`reefblast-bot`), dropping container RAM consumption by **~85%** (from 38 MiB to **5.7 MiB** per bot) and reducing CPU usage to **<0.4%**.

### Active Containers & Fleet Overview

| Container | Role / Identity | Level & Vessel | Equipped Weapon | Zone | RAM Usage | Status |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `cuangbot` | Discord Control Hub | Bot #2600 | N/A | Harbor / API | ~90 MiB | Online |
| `reefblast-bot` | Captain `cuang-bot` | Lv 100 · `t6-def` | Parasite Pistol (T5 Leech) | Zone 18 | **5.3 MiB** | Farming (Combo > 5,000) |
| `reefblast-bot-william` | Captain `william` | Lv 121 · `x-warlord` | Kraken's Thirst (T6 Leech) | Zone 19 | **5.7 MiB** | Farming (Combo > 3,000) |
| `reefblast-bot-capt_1a74cb` | Captain `capt_1a74cb` | Lv 7+ · `boat-fishing-small` | Default Blaster | Zone 0+ | **5.5 MiB** | Farming |
| `reefblast-bot-capt_9aac46` | Captain `capt_9aac46` | Lv 12+ · `boat-fishing-small` | Default Blaster | Zone 3 | **5.8 MiB** | Farming (Combo > 500) |
| `reefblast-bot-capt_06e0d0` | Captain `capt_06e0d0` | Lv 12+ · `boat-fishing-small` | Default Blaster | Zone 3 | **6.5 MiB** | Farming (Combo > 550) |
| `reefblast-bot-capt_f2adb3` | Captain `capt_f2adb3` | Lv 13+ · `boat-fishing-small` | Default Blaster | Zone 3 | **5.8 MiB** | Farming (Combo > 470) |
| `reefblast-bot-capt_5363b5` | Captain `capt_5363b5` | Lv 14+ · `boat-fishing-small` | Default Blaster | Zone 3 | **5.9 MiB** | Farming (Combo > 500) |

---

## 2. Comprehensive Changelog

### Phase 1: Go Native Daemon Migration
- **Rationale**: Node.js V8 runtime consumed ~35–40 MiB per bot with garbage collection spikes, limiting fleet scalability.
- **Implementation**: Built a standalone, statically linked Go binary (`reefblast-bot`) in `/home/agent/orca/learnings/reefblast-go/` using `gorilla/websocket`.
- **Schema Hardening**: Resolved WebSocket JSON unmarshaling collisions:
  - `FishDef.Boss` parsed as `bool` (instead of string/int).
  - `GameDefs.ZonePower` parsed as `[]float64`.
  - `IncomingMessage.Party` typed as `interface{}` to handle both string IDs, objects, and numbers across snapshots and room events.
  - `MarketListing.ID` typed as `int64`.

### Phase 2: Memory Optimization to Ultra-Lean Footprint (5.7 MiB)
- **Thread Constriction**: Configured `runtime.GOMAXPROCS(1)` in `main.go`, preventing Go from spawning runtime thread stacks across all 4 host CPU cores.
- **Aggressive GC**: Set `debug.SetGCPercent(25)` (default is 100%) to trigger GC earlier when heap allocation is low.
- **Memory Soft-Limit**: Added `debug.SetMemoryLimit(16 * 1024 * 1024)` to bound the Go memory arena.
- **Kernel Page Reclamation**: Added periodic calls to `debug.FreeOSMemory()` inside `statusDumperLoop` (every 30s) to instruct the Linux kernel to release unused arena memory via `MADV_DONTNEED`.

### Phase 3: Combat Targeting & Torpedo Prioritization Overhaul
- **Root Cause of Stagnant Progression**:
  - The previous targeting logic intercepted *any* incoming boss torpedo with `s.Z > 0.5`. In Zone 18 and 19, bosses constantly fire torpedoes.
  - Both bots were intercepting **2,400+ torpedoes per 15 minutes**. Torpedoes give **0 XP, 0 Gold, and 0 Loot**, which starved fish DPS and froze level progression.
- **Resolution**:
  - Prioritized fish and boss targets first.
  - Torpedo interception now only activates if **boat HP is critically low** (`p[6] < 350`, i.e. <35% HP) or if **no fish are present on screen**.
  - Since both boats carry high-tier hulls (>118k–130k base HP, >316–348 DEF) and lifesteal weapons, continuous fish DPS sustains hull health via leech healing while preserving combos and XP.

### Phase 4: AM-GM Multiplicative Skill Formula & Lifesteal Synergy
- **Fixed Zero-Ratio Bug**:
  - In `autoAllocateSkills()`, when skills were at 0, `0 / weight == 0` for all stats. Due to Go map iteration ordering and `toSpend := points`, the first stat picked received 50 points immediately.
  - This left Cuang with `hp: 50, aspd: 49, atk: 0, crit: 0` and William with `hp: 50, atk: 50, aspd: 20, crit: 0`.
- **Lifesteal Math**:
  - Weapons with `leech: 0.012` heal **1.2% of max HP per hit**.
  - Investing 50 points into HP provides zero extra healing rate. Offense (`ATK`, `ASPD`, `CRIT`) multiplies both damage output and leech proc frequency.
- **Multiplicative DPS**:
  $$\text{DPS Multiplier} = (1 + 0.03 \cdot \text{ATK}) \times (1 + 0.03 \cdot \text{ASPD}) \times (1 + \text{BaseCrit} + 0.01 \cdot \text{CRIT})$$
- **New Point-by-Point Allocation**:
  $$\text{Score}(s) = \frac{\text{CurrentPoints}(s) + 0.5}{\text{Weight}(s)}$$
  - Ratios for Lifesteal builds: `ATK: 0.38`, `ASPD: 0.36`, `CRIT: 0.18`, `DEF: 0.04`, `REGEN: 0.03`, `HP: 0.01`.
  - Respecced Cuang (Lv 100) to: `ATK: 37, ASPD: 36, CRIT: 18, DEF: 4, REGEN: 3, HP: 1` (+128% effective DPS).
  - Respecced William (Lv 121) to: `ATK: 45, ASPD: 43, CRIT: 22, DEF: 5, REGEN: 4, HP: 1` (+135% effective DPS).
- **Auto-Respec**: Implemented `checkNeedsRespecLocked()` to automatically trigger `{"t": "respec"}` in harbor whenever an old or sub-optimal build is detected.

### Phase 5: Kill Tracking & Snapshot Event Parsing
- Parsed `s.EV` snapshot events in `handleSnapshot()`:
  - Evaluates `e.K == "k"` and matches player slot against `e.S` or `e.Loot` records (`l[0] == slot && l[1] > 0`).
  - Correctly increments `d.stats.KillsThisSession`.
- Corrected player sink detection using `p[11]` (boolean dead state) rather than `p[10]` (countdown float).

### Phase 6: Multi-Type Bot Provisioning via Discord (`cuangbot`)
- Implemented 3 bot provisioning pathways in `discord-bot.js`:
  1. **Define Credentials**: Manual login with user email/password.
  2. **Random Account Generator**: Generates random captain name, email (`@cuang.dev`), and password; registers via `/api/register` with automated name collision retry.
  3. **Email List Registration**: Accepts a single email or list of comma/newline-separated emails, registers each with generated secure passwords, and provisions containers.
- **Discord UI**: Added interactive modal triggers (`rb:open_modal:manual`, `rb:open_modal:random`, `rb:open_modal:emails`) and Discord spoiler password masking (`||password||`).
- Container factory auto-copies the compiled Go binary (`reefblast-bot`) into every new container upon provisioning.

### Phase 7: Remote Docker LXC Host Sync & Resilient Provisioning
- **Root Cause of Failed Provisioning (`lstat /home/agent/orca/learnings/reefblast-go: no such file or directory`)**:
  - The local Docker CLI operates against the remote LXC host (`docker-host` / `10.20.20.11`).
  - Container `cuangbot` executes `docker cp /home/agent/orca/learnings/reefblast-go/reefblast-bot ...` using client-side paths on the Docker host.
  - The `reefblast-go/` directory existed only on the local dev machine and had not been synced to `docker-host`.
  - Furthermore, `cuangbot` did not have `/home/agent/orca` mounted, isolating bot directories (`credentials.txt`, `policy.json`) inside ephemeral container storage.
- **Resolution**:
  - **Volume Mount**: Added `- /home/agent/orca:/home/agent/orca` into `docker-compose.discord.yml`.
  - **Remote Sync**: Rsynced `reefblast-go` source & binary, `scripts/`, and bot workspaces directly to `docker-host:/home/agent/orca/learnings/`.
  - **Multi-Tier Daemon Sync**: Enhanced `deployBotDaemon()` in `discord-bot.js` with a 4-tier fallback:
    1. Host path (`/home/agent/orca/learnings/reefblast-go/reefblast-bot`)
    2. Local container path (`/app/reefblast-bot`)
    3. Template container stream (`reefblast-bot:/app/reefblast-bot`)
    4. Base image fallback (`reefblast-docker-reefblast-bot:latest`)
  - **Resilient Policy Writing**: Policy is streamed directly via `docker exec -i ... sh -c 'cat > /app/policy.json'`, eliminating filesystem dependency errors.
  - **Base Image Rebuilt**: Rebuilt `reefblast-docker-reefblast-bot:latest` with the latest Go binary (Oct 5 23:19 build) so new containers launch with the updated binary out of the box.
  - **Account Recovery**: Extracted credentials for the 5 failed accounts (`capt_1a74cb`, `capt_9aac46`, `capt_06e0d0`, `capt_f2adb3`, `capt_5363b5`), synced policies and Go binaries, and restored active farming.

### Phase 8: Directory Restructuring & Asset Organization
- **AudioDiff**: Moved all 6 loose screenshot assets (`audiodiff_default.png`, `audiodiff_pr242_security_audit.png`, `audiodiff_slide1.png`, `audiodiff_slide4_assemblyai.png`, `audiodiff_universal2_walkthrough.png`, `audiodiff_voice_inquiry.png`) to `assemblyai-voice-agent-hackathon/docs/screenshots/`.
- **Projects Portal**: Moved `root_portal.png` to `projects-cuang-dev/docs/screenshots/`.
- **Reefblast Legacy Archive**:
  - Synced latest `calcWeaponScore()` updates into `reefblast-docker/reefblast_auto_progression.js`.
  - Archived legacy Node.js scripts (`reefblast_afk_bot.js`, `reefblast_ws_client.js`) into `reefblast-docker/legacy/`.
  - Removed stale files from root: `reefblast_bot_base64.txt`, `auto_progression.log`, `auto_progression.json`, and root `credentials.txt`.
- **Bundle Cleanup**: Removed stray `agent-catalog-HsKZoB4t.js`.

### Phase 9: Unlimited Fleet Scaling & Mass Range Bot Disabling
- **Removal of All Fleet Limits**:
  - Eliminated the 5-bot limit in `provisionRandomBots()` and 10-email limit in `provisionEmailList()`. Fleet generation is now unlimited, bounded only by host resources.
  - Increased container name collision bound from 25 to 1000 in `deployBotDaemon()`.
  - Updated slash command options and modal text inputs (`rand_count` length increased from 2 to 4 digits).
- **Deterministic 1-Based Fleet Indexing**:
  - `listFleetBots()` guarantees stable 1-based order: `#1: cuang`, `#2: william`, followed by `#3..N` sorted alphabetically (`capt_*`).
- **Flexible Target Expression Resolver (`resolveBotTargets`)**:
  - **Index Ranges**: `3-7`, `3..7`, `3:7` (matches bots at index 3 through 7 inclusive).
  - **Open-Ended Ranges**: `3-`, `3+`, `>=3` (matches bot 3 and all higher indices).
  - **Upper Bounds**: `-5`, `<=5` (matches bots 1 through 5).
  - **Single Index**: `3`, `#3` (resolves to bot at index 3).
  - **Delimited Combinations**: `1, 2, 5`, `3-5, 7` (combines ranges and single indices).
  - **Glob Wildcards**: `capt*`, `capt_*` (matches container name or bot key patterns).
  - **Semantic Keywords**:
    - `non-core` (or `extra`, `generated`): Target all generated bots (#3..N) while protecting main captains Cuang (#1) and William (#2).
    - `core`: Targets only Cuang (#1) and William (#2).
    - `all` (or `*`): Targets the entire fleet.
- **Concurrent Bulk Docker Stop/Start**:
  - Replaced sequential one-by-one container stops with concurrent multi-argument calls: `docker stop <c1> <c2> ...` and `docker start <c1> <c2> ...`.
  - Reduces bulk shutdown time from 15–20s down to 1–2s.
- **Scalable Monospace Fleet Embed (Discord 25-Field Guard)**:
  - Redesigned `buildReefblastFleetEmbed()` to automatically pivot from individual card fields to chunked monospace tables (`# | State | Captain | Level | Zone | Memory`) when fleet $>5$, completely eliminating Discord's `RangeError: Embed fields must be <= 25`.
- **Interactive UI Mass Controls**:
  - Dynamic button toggles: `Disable All (N)` / `Enable All`.
  - Dedicated one-click quick-action button: `Stop Gen (#3+)` (`rb:stop_non_core`) for instant deactivation of generated bots without risking main accounts.
  - Interactive modal button: `Mass Range...` (`rb:open_modal:mass_control`) allowing Discord users to submit any expression (`3-7`, `3+`, `capt_*`, `non-core`) on the fly.
  - Updated `/reefblast stop`, `/reefblast start`, and `/reefblast toggle` slash commands to accept any target expression.

### Phase 10: Smooth Sailing Physics & Navigation Engine Overhaul
- **Root Cause Analysis of Sailing Hitching**:
  1. **Server Position Desync & 80-Unit Rubberbanding**: `IncomingMessage` lacked a `pos: [x, z]` field in `types.go`, and `case "hello"` / `case "left"` hardcoded `d.pos = (0, 10)`. When exiting Zone 18 at `(43.8, 93.9)`, the bot believed it was at `(0, 10)` and sent movement packets from `(0, 10)`. The server rejected the instantaneous 80-unit teleport, triggering a rubberband correction (`t: "pos"`) that snapped the vessel back.
  2. **Solid Dock Collision Ramming**: All dock targets (`(9, -4)`, `(-9, -4)`, `(26, -9)`, `(-26, -9)`) were inside the solid dock collision bounding boxes (`cx = a.x, cz = a.z - 1.5`, half-width `6.5` in X and `4.8` in Z). The boat sailed directly into the solid structure, causing the server collision solver to constantly repel it until the 6-second timeout.
  3. **Illegal Speed Multiplier**: The Go daemon had hardcoded `speed := 18.0` for all vessels, whereas `t6-def` has a max speed of `10.5` and starter vessels `9.5`. The server's movement validator flagged speed limit violations and snapped the boat back.
  4. **Island 0 Path Intersection**: Direct routes between North harbor gates (7–12) and South docks passed directly through Island 0 `(0, -32, r=17)` with no obstacle routing, ramming the island.
  5. **Queued Ticker Re-entry Glitch**: Ticker backlog in `managerLoop` fired a second `managerStep` while `d.zone` was still nil during WebSocket transit, queuing a duplicate gate sail and zone enter at the same millisecond.
  6. **Zero-Inertia Movement**: Straight position increments without exponential velocity damping ($k = 1 - e^{-dt \cdot 2.4}$) or heading rotation ($r \to \text{atan2}(vx, vz)$) produced jagged 10 Hz discrete steps in other clients.
- **Resolution**:
  - **Schema Update**: Added `Pos []float64`, `HarborArea`, `HarborIsland` to `types.go`. `case "hello"` and `case "left"` now properly read `m.Pos`.
  - **Physics Engine (`stepHarborPhysics`)**: Implemented continuous velocity vectors `vx, vz`, exponential damping ($k = 1 - e^{-dt \cdot 2.4}$), dock bounding box resolution, island collision sliding, and heading turning matching the official Three.js client (`harborStep`).
  - **Dynamic Speed**: Vessel speed is now dynamically derived from `defs.boats` (`boat.speed`), eliminating server speed violations.
  - **Waypoint Pathfinding (`getWaypoints`)**: Automatically detects paths intersecting Island 0 and inserts intermediate waypoints via East `(28, -32)` or West `(-28, -32)` channels.
  - **Open-Water Dock Interaction Points**: Relocated all area navigation targets outside solid dock collision boxes: Market `(9, -1.0)`, Tavern `(-9, -1.0)`, Shipyard `(26, -6.5)`, Forge `(-26, -6.5)`.
  - **Zone Entry Guard**: Added `isEnteringZone` guard and converted `managerLoop` to sleep-after-step, completely preventing duplicate execution.
  - **Rebuilt & Deployed**: Recompiled Go binary, updated base image `reefblast-docker-reefblast-bot:latest`, and live-updated all 7 containers.

---

## 3. Architecture & Key Paths

```
/home/agent/orca/
├── learnings/
│   ├── reefblast-go/                         # Go Daemon Source & Binary
│   │   ├── main.go                           # Entrypoint, runtime memory config (GOMAXPROCS, GC, MemLimit)
│   │   ├── daemon.go                         # Core logic: WS loop, combat aim, AM-GM skills, harbor, market
│   │   ├── types.go                          # Game schemas (WeaponDef, BoatDef, PlayerMe, IncomingMessage)
│   │   └── reefblast-bot                     # Compiled 5.6MB static ELF binary
│   │
│   ├── reefblast-docker/                     # Cuang Container Workspace
│   │   ├── logs/status.json                  # Live Cuang status dump (read by Discord bot & monitoring)
│   │   ├── logs/auto_progression.log         # Real-time event log
│   │   └── policy.json                       # Runtime toggles: buying, salvaging, market selling
│   │
│   └── reefblast-docker-william/             # William Container Workspace
│       ├── logs/status.json                  # Live William status dump
│       ├── logs/auto_progression.log         # Real-time event log
│       └── policy.json                       # Runtime toggles
│
└── AI-nimate/
    └── scripts/
        └── discord-bot.js                    # Discord Control Bot (Cuangbot#2600)
```

---

## 4. Key Mechanics & Protocol Reference

### WebSocket Message Formats (`wss://greentea.id/reefblast/ws?token=...`)

| Type (`t`) | Direction | Payload Example | Purpose |
| :--- | :--- | :--- | :--- |
| `aim` | Client $\to$ Server | `{"t":"aim","x":12.5,"z":4.2,"f":true}` | Set aiming coordinates and fire status |
| `s` | Server $\to$ Client | `{"t":"s","p":[...],"f":[...],"sh":[...],"ev":[...]}` | World snapshot (30 ticks/sec) |
| `skill` | Client $\to$ Server | `{"t":"skill","id":"atk","n":5}` | Allocate skill points |
| `respec` | Client $\to$ Server | `{"t":"respec"}` | Reset all skills to points in harbor |
| `zone` | Client $\to$ Server | `{"t":"zone","z":18}` | Sail into target zone gate |
| `leaveZone` | Client $\to$ Server | `{"t":"leaveZone"}` | Return to Harbor |
| `mkBuy` | Client $\to$ Server | `{"t":"mkBuy","id":12345}` | Purchase market listing |
| `salvage` | Client $\to$ Server | `{"t":"salvage","uids":["..."]}` | Bulk salvage items for scrap |
| `equipWeapon` | Client $\to$ Server | `{"t":"equipWeapon","id":"..."}` | Equip blaster UID |
| `equipBoat` | Client $\to$ Server | `{"t":"equipBoat","id":"x-warlord"}` | Equip boat hull |

### Player Snapshot Index Schema (`p` in `s.P`)
- `p[0]`: Player Slot (int)
- `p[1]`: Character model / ID
- `p[2]`: Boat ID
- `p[3]`: Vessel Name
- `p[4]`: Current Combo count (float64)
- `p[5]`: Score / Coins
- `p[6]`: Hull HP in tenths of percent ($1000 = 100\%$, $350 = 35\%$)
- `p[10]`: Respawn countdown timer (seconds)
- `p[11]`: Sunk / Dead flag (`bool`)

---

## 5. Operations & Runbook

### Rebuilding and Deploying the Go Daemon
Whenever changes are made to `main.go`, `daemon.go`, or `types.go`:

```bash
cd /home/agent/orca/learnings/reefblast-go
go build -ldflags="-s -w" -o reefblast-bot .

# Copy binary to local container contexts
cp reefblast-bot /home/agent/orca/learnings/reefblast-docker/reefblast-bot
cp reefblast-bot /home/agent/orca/learnings/reefblast-docker-william/reefblast-bot

# Hot-copy into live containers and restart
docker cp reefblast-bot reefblast-bot:/app/reefblast-bot
docker cp reefblast-bot reefblast-bot-william:/app/reefblast-bot
docker restart reefblast-bot reefblast-bot-william
```

### Inspecting Fleet Health & Status
```bash
# Live RAM & CPU metrics across all containers
docker stats --no-stream reefblast-bot reefblast-bot-william cuangbot

# Quick status check on both captains
docker exec reefblast-bot cat logs/status.json | jq '{name: .me.name, level: .me.level, xp: .me.xp, kills: .stats.killsThisSession, bestCombo: .stats.bestCombo, skills: .me.skills, zone: .zone}'
docker exec reefblast-bot-william cat logs/status.json | jq '{name: .me.name, level: .me.level, xp: .me.xp, kills: .stats.killsThisSession, bestCombo: .stats.bestCombo, skills: .me.skills, zone: .zone}'

# Inspect live logs
docker logs --tail 30 reefblast-bot
docker logs --tail 30 reefblast-bot-william
```

### Discord Bot Operations
- The Discord bot is running in container `cuangbot`.
- Syncing changes from `/home/agent/orca/AI-nimate/scripts/discord-bot.js`:
```bash
docker cp /home/agent/orca/AI-nimate/scripts/discord-bot.js cuangbot:/app/scripts/discord-bot.js
docker restart cuangbot
```

---

## 6. Known Considerations for Future Agents

1. **Auto-Respec Guard**:
   - `checkNeedsRespecLocked()` guards against loop respecs:
     - `if d.hasAutoRespecced { return false }` guarantees at most one respec per daemon session.
     - `if d.me.Points > 0 { return false }` prevents newly respecced bots with unspent points from looping.
     - `if d.me.RespecCost > 0 && d.me.Coins < d.me.RespecCost { return false }` prevents broke bots from leaving zones for unaffordable respecs.
     - `hp > 5` check is only evaluated if offensive stats are not yet capped: `(atk < 50 || aspd < 50 || crit < 50)`. High-level accounts (Lv > 155) max all offensive stats (50/50/50/5) and naturally overflow points into DEF and HP. Without this guard, high-level bots get stuck in an infinite 230,000 coin respec drain loop.
2. **Torpedo Defense Threshold**:
   - Only modify `hpCritical` (`d.hp < 350.0`) if the player swaps away from a lifesteal weapon to a non-leech weapon with low defense. With `parasite` or `krakenthirst`, keeping torpedo interception low is essential for XP velocity.
3. **Market Sniper Safety**:
   - `checkHullUpgrade()` strictly evaluates `(effTier * 20000) + (ehp * 0.5)` to ensure a bot never downgrades its vessel.
   - Any new weapons added to `defs` are dynamically scored by `CalcWeaponScore()`.

---

## 7. Phase 11: Ultra-Lean Memory Footprint & Zero-Allocation Parser

### Architecture & Optimization Rationale
1. **The Root Bottleneck in High-Frequency Snapshot Parsing**:
   - Server WebSocket snapshots (`t: "s"`) arrive 30 times per second with arrays of fish `f`, boss shots `sh`, players `p`, and events `ev`.
   - Standard Go `json.Unmarshal` into generic `[][]interface{}` caused interface boxing (`runtime.convT64`) for every float64 coordinate and stat $>255$, churning through **~15,000 heap allocations per second** (~150 KB/s heap churn per bot).
   - `seenFish := make(map[int64]bool)` and `seenShots := make(map[int64]bool)` allocated 60 new hash tables per second inside `handleSnapshot`.

2. **Zero-Allocation Snapshot Scanner (`snapshot.go`)**:
   - Implemented a custom token scanner (`SnapshotScanner`) that directly reads numbers using `unsafe.String` and `strconv.ParseFloat` / `strconv.ParseInt`.
   - Properly tracks `inQuote` states to prevent nested quoted strings (e.g. player names with commas) from breaking array scanning.
   - Benchmarks verified via `snapshot_test.go`:
     - Standard `json.Unmarshal`: `23,867 ns/op` | `4,976 B/op` | `184 allocs/op`
     - `SnapshotScanner.Parse`: **`2,828 ns/op` (8.4x faster)** | **`0 B/op`** | **`0 allocs/op`**
   - In `daemon.go`: Fast path checks `bytes.Contains(message[:40], []byte(`"t":"s"`))` and executes `handleSnapshotFast` directly, bypassing `json.Unmarshal` for 99.9% of WebSocket traffic.

3. **Generation Stamping Pattern**:
   - Added `snapshotGen uint64` to `ReefBlastDaemon` and `LastSeenGen uint64` to `FishState` and `ShotState`.
   - When a fish or shot is updated, its `LastSeenGen` is stamped with the current `d.snapshotGen`.
   - Entities whose `LastSeenGen != d.snapshotGen` are evicted in a single pass over existing maps with zero map allocations or re-hashes.

4. **Runtime & Kernel Memory Calibration**:
   - `runtime.GOMAXPROCS(1)`: Constrains OS thread creation and stack overhead to 1 M-thread.
   - `debug.SetGCPercent(50)` + `debug.SetMemoryLimit(12 * 1024 * 1024)`: Proactive Go runtime scavenger reclaims pages without GC CPU thrashing.
   - Reduced `debug.FreeOSMemory()` frequency to once every 60s, letting Go's steady-state allocator handle normal recycling.
   - Linux smaps Page Cache sharing: Statically compiled with `-trimpath -ldflags="-s -w"`. Physical memory for the 5.5MB ELF `.text` pages is shared in kernel Page Cache across all containers derived from `reefblast-docker-reefblast-bot:latest`. UPX was intentionally avoided because UPX decompresses into private dirty anonymous memory, destroying Page Cache deduplication.

### Measured Impact Across Live Fleet
- **Container Resident Memory (RSS)**: Reduced from **~11–12 MiB down to 4.8–5.9 MiB**.
- **CPU Utilization per Bot**: Dropped from **~3.6% down to 0.35%–0.5%** (~85–90% CPU reduction due to eliminated GC pressure).
- **Gameplay Integrity**: Verified on live servers with Cuang (Zone 18, 700+ combo, 16+ kills/min) and William (Zone 19, 800+ combo, 54+ kills/min).

---

## 8. Phase 12: Granular Fleet Controls & Index-Based Profiles

### Overview & Architecture
The Discord bot daemon (`cuangbot` in `/home/agent/orca/AI-nimate/scripts/discord-bot.js`) was overhauled to provide granular fleet management without UI clutter:

1. **Streamlined Fleet View**:
   - **Removed**: Cluttered main-view buttons (`Stop Gen (#3+)`, `Disable Cuang`, `Disable William`, and `Lock William Purchases`). Specific bot overrides are now managed inside that bot's profile.
   - **Added Granular Start/Stop**:
     - `▶️ Start All`: Boots all daemons across the fleet.
     - `⏹️ Stop All`: Disables all daemons across the fleet.
     - `🎯 Start by Index...`: Interactive modal accepting single indices (`3`), ranges (`3-7`, `3-`), or lists (`1,2,5`).
     - `🛑 Stop by Index...`: Interactive modal accepting single indices or ranges to stop target daemons.

2. **Universal Profiles by Index**:
   - Every daemon in the fleet (from `#1` Cuang to `#N` generated bots) now has a full Profile view identical to William and Cuang.
   - **Direct Profile Select Dropdown**: A native Discord `StringSelectMenuBuilder` dropdown (`🔍 Jump to Bot Profile (#1 to #N)...`) renders on both Fleet View and individual profile views, displaying `#Index Name (Level) | Zone | Coins | 🟢/🔴 State` for 1-click profile switching.
   - **Slash Command**: `/reefblast profile target:<index or name>` (e.g. `/reefblast profile target:3` or `/reefblast profile target:william`).
   - **Profile Controls**:
     - Power Toggle: `▶️ Enable / 🛑 Disable` this bot.
     - Policy Toggles: `Buying: ON/OFF`, `Salvage: ON/LOCK`, `Selling: ON/OFF`, `Lock All / Enable All`.
     - Live Activity Logs: Live tail of `/app/logs/auto_progression.log` for this specific bot.
     - Navigation: `◀️ Prev (#X)` and `Next (#Y) ▶️` buttons to quickly flip between bot profiles in sequence.

---

## 9. Phase 13: High-Performance Async Telemetry Engine & Latency Optimization

### Root Cause Analysis of Sluggish Discord Responses
Profile loads and commands previously took **15 to 30 seconds** due to two cascading bottlenecks in `/home/agent/orca/AI-nimate/scripts/discord-bot.js`:
1. **Synchronous `docker stats --no-stream`**: Spawning `docker stats` sequentially across 10 containers blocked the single-threaded Node.js event loop for ~2.6 seconds per container (~26 seconds total).
2. **Synchronous `execSync` & Sequential Spawns**: Every interaction and profile load called `buildProfileSelectRow`, which mapped over all bots and ran `docker inspect`, `docker exec cat`, and `docker stats` synchronously. This starved Discord Gateway heartbeats, causing `DiscordAPIError[10062]: Unknown interaction` timeouts.

### Architectural Solution
1. **Native Docker UNIX Socket Client (`dockerRequest`)**:
   - Swapped out CLI child process spawns (`execSync`) for asynchronous HTTP requests over the mounted `/var/run/docker.sock` UNIX domain socket via Node's native `http` module.
   - Container listing (`GET /containers/json?all=1`) executes in **~10–15 ms** (down from ~3,000 ms).
   - Reading `status.json` via Docker archive endpoint (`GET /containers/{id}/archive?path=/app/logs/status.json`) executes in **~25–40 ms** (down from ~2,500 ms).
2. **Parallel Fleet Status Scanning (`fetchAllBotStatuses`)**:
   - Scans the entire fleet asynchronously in parallel (`Promise.all`), reducing full fleet telemetry acquisition from **~26,000 ms down to ~100–120 ms** (>200x speedup).
   - In-memory cache with 4-second TTL serves consecutive profile navigation and button interactions in **0–5 ms**.
3. **Async Non-Blocking Start / Stop / Logs**:
   - `startBotContainers` and `stopBotContainers` execute concurrently via `POST /containers/{id}/start` and `POST /containers/{id}/stop?t=2` in **~250–350 ms** across the whole fleet (down from 35s).
   - Live activity logs fetched asynchronously via `GET /containers/{id}/logs?tail=15` in **~50 ms**.
   - Server stats (`getServerStats`) queries Docker API non-blockingly in **~10 ms**.
4. **Performance Verification**:
   - Profile embed generation: **1 ms** (previously ~26s).
   - Fleet telemetry embed generation: **5–6 ms** (previously ~26s).
   - Complete action row generation: **3 ms**.
   - Zero Discord Gateway event-loop starvation; interaction timeouts completely eliminated.

---

## 10. Phase 14: Combat Survivability, Zero-Allocation Aiming, Crew Optimization & Smart Harbor Voyage Engine

### 1. Root Causes of Stagnant Progression & Death Loops
1. **The Zone 19 Doom Orb & Non-Lifesteal Sinking Loop**:
   - High-tier bosses (`boss19` Leviathan King) cast `traits: ["barrage", "doom", "berserk"]`.
   - The bot engine had an unconditioned guard `hpCritical := d.hp > 0 && d.hp < 350.0`.
   - When Cuang crafted `levispread` (Tier 6 weapon with **no lifesteal/leech**), the ship had **0% healing** on fish attacks. Torpedoes and Doom Orbs chipped hull health down without interception, leading to fatal boat sinkings every 40–70 seconds.
   - Doom Orbs (`kind == 1` or `"doom"`) were completely ignored when HP $\ge 350$, instantly wiping ships.
2. **The 2-Second Harbor Return Glitch (Voyage Timer Bug)**:
   - `timeAtSea := time.Since(d.lastHarborTrip).Seconds()` was evaluated against `> 240s`.
   - However, `d.lastHarborTrip` was ONLY updated when an `[UPGRADE]` harbor trip occurred, NOT when the server sent `case "left"` or `case "joined"`.
   - Consequently, when a bot entered a zone after sinking or clearing, `timeAtSea` was already $> 240\text{s}$, triggering an immediate, wasteful harbor return 2 seconds later.
   - Furthermore, `routineShop` triggered every 4 minutes just because `Coins > 1,500,000` even when the bot already possessed maximum-tier vessels, weapons, and helpers, wasting 25+ seconds of travel time every 4 minutes.
3. **Crew Helper Sub-Optimization**:
   - The Go daemon lacked `autoEquipBestHelper()`. William owned `nova` (Admiral Nova, 3,284 DPS, homing stars) but remained stuck with `rook` (1,830 DPS) equipped.
4. **Aim Serialization Allocation Overhead**:
   - `pushAim` ran at up to 20 Hz, allocating a `map[string]interface{}` and invoking reflection-based `json.Marshal` on every frame, generating **~240 heap allocations/sec** per bot daemon.

### 2. Architectural Solutions Implemented
1. **Lifesteal Synergy & Doom Orb Priority Defense (`combatAim`)**:
   - Added Doom Orb classification (`s.Kind == "doom" || s.Kind == "1"`). Doom Orbs are **always intercepted with absolute priority** regardless of current hull health.
   - Added dynamic lifesteal capability detection:
     - If `hasLeech == true`: Ship sustains HP via offensive DPS; torpedo interception threshold stays lean (`d.hp < 400.0` or no fish).
     - If `hasLeech == false`: Ship cannot self-heal; activates proactive torpedo interception (`s.Z > 0.0` or `d.hp < 800.0`).
   - Filtered warded (`Flags&8`), shielded (`Flags&1`), and vanished stalkers (`Flags&64`).
   - Lead aim math now incorporates `bulletSpeed` directly for projectile interception.
2. **Zero-Allocation Aim & Ping Pipeline (`sendAimRaw` & `sendPingRaw`)**:
   - Implemented direct stack-buffer formatting for the high-frequency 20 Hz aim packets using `strconv.AppendFloat`:
     - Standard `json.Marshal`: `2,067 ns/op` | `672 B/op` | `12 allocs/op`
     - `sendAimRaw`: **`165.6 ns/op` (12.5x faster)** | **`0 B/op`** | **`0 allocs/op`**
   - Implemented zero-allocation `sendPingRaw`.
3. **Smart Harbor Voyage Engine & Upgrade Detection (`hasPendingUpgrades`)**:
   - Added `d.zoneEnteredAt = time.Now()` and reset `d.lastHarborTrip` on all zone transitions.
   - Implemented `hasPendingUpgrades()`, evaluating whether better boat hulls, weapons, or helpers are affordable, or if inventory is full ($\ge 5$ items).
   - Bots now remain in combat uninterrupted unless an upgrade is actually affordable, increasing combat farming uptime to **>95%**.
4. **Auto-Equip Crew Helper (`autoEquipBestHelper`)**:
   - Implemented automated helper evaluation based on DPS and power level. William automatically equipped Admiral Nova (+80% helper DPS).
5. **Daily Island Challenge Automation (`checkNeedsIslandRun`)**:
   - Automatically navigates to Boss Rush islet `(-130.9, -102.3)` and executes `{"t": "modeEnter", "mode": "rush", "tier": 0}` when daily quests require an island challenge, unlocking the 93k coin + 488k XP daily bonus.
6. **Container Resource Footprint & Log Rotation**:
   - Lowered container provisioning limits in `discord-bot.js` from `192m` to `--memory 64m --log-opt max-size=10m --log-opt max-file=2`.
   - Added automatic 10 MiB log rotation in `d.Log()` (`auto_progression.log.1`).

### 3. Live Production Validation
- **William**: Reached **Level 130** in Zone 19 with combo sustained past **2,000+**, farming over **48,000 coins/min** with Nova equipped. Memory RSS: **6.07 MiB**, CPU: **0.57%**.
- **Cuang**: Successfully sustained combat in Zone 19 with non-leech `levispread` without sinking; intercepted **129+ enemy torpedoes and Doom Orbs**, combo maintained continuously. Memory RSS: **6.28 MiB**, CPU: **0.46%**.

---

## 11. Phase 15: Natural Player Bot Name Generator & Combinatorial Deduplication

### 1. Problem Statement
Previously, bot provisioning generated obvious, artificial bot identifiers formatted as `capt_<6-hex>` (e.g. `capt_1a74cb`, `capt_06e0d0`, `capt_cd40e6`). These stood out conspicuously in multiplayer lobbies and lacked immersion.

### 2. Implementation Overview
In `/home/agent/orca/AI-nimate/scripts/discord-bot.js`, the naming engine was overhauled with a natural gamer/naval name generator:
1. **Curated Multi-Category Dictionaries**:
   - `BOT_NAME_ADJECTIVES`: 321 evocative descriptors (e.g. *Swift*, *Salty*, *Fierce*, *Crimson*, *Stormy*, *Abyssal*, *Nocturnal*).
   - `BOT_NAME_NOUNS`: 922 marine creatures, vessels, nautical gear, and ocean phenomena (e.g. *Marlin*, *Kraken*, *Dreadnought*, *Vessel*, *Swordfish*, *Cyclone*).
   - `BOT_NAME_TITLES`: 39 naval and pirate titles (e.g. *Capn*, *Admiral*, *Skipper*, *Chief*, *Baron*, *Duke*).
   - `BOT_NAME_PEOPLE`: 93 classic sailor and gamer names (e.g. *Jack*, *Barnaby*, *Flint*, *Drake*, *Morgan*, *Finn*).
   - `BOT_NAME_ROLES`: 32 naval role suffixes (e.g. *Chaser*, *Rider*, *Hunter*, *Slayer*, *Breaker*, *Scout*).
   - `BOT_NAME_SOLO`: 36 solo pirate & mythic monikers (e.g. *Nautilus*, *Leviathan*, *Maelstrom*, *Blackwater*, *Shipwreck*).
2. **Naming Combinations & Character Constraint ($\le 16$ Chars)**:
   - Primary patterns:
     - 50% `[Adjective][Noun]` (e.g. `ChargingHerring`, `EmeraldNate`, `MoonlitSwordfish`)
     - 15% `[Title][Person]` (e.g. `OldSeth`, `DukeGulliver`, `CapnJack`)
     - 15% `[Adjective][Person]` (e.g. `ResoluteVictor`, `NobleCalico`, `EagerRusty`)
     - 10% `[Noun][Role]` (e.g. `AukSlayer`, `StraitBreaker`, `WaveRider`)
     - 5% `[Title][Noun]` (e.g. `DocWeatherfish`, `AdmiralShark`)
     - 5% `[Solo]` (e.g. `Blackwater`, `Shipwreck`, `Cannonball`)
   - Yields over **250,000+ natural combinations** strictly $\le 16$ alphanumeric characters without chopping letters mid-word.
3. **Four-Tier Deduplication Guarantee**:
   - Scans active containers via Docker socket (`listFleetBots()`).
   - Scans stored credentials across host directories (`/home/agent/orca/learnings/reefblast-docker-*`).
   - Tracks in-memory `usedNames` Set across batch generations.
   - On server-side `"name is taken"` API response in `registerAccountWithRetry()`, re-rolls fresh natural combinations before falling back to clean 2-digit gamer suffixes (`Base99`).
4. **UI Integration**:
   - `rb:open_modal:random`: Removed forced default `'capt'`, added optional style/prefix placeholder.
   - Slash command `/reefblast add mode:random`: Defaults to clean natural names unless a custom prefix is explicitly supplied.

---

## 12. Phase 16: Organic Naval Hydrodynamics, Fleet Distribution & Human Aim Engine

### 1. Problem Statement
Previously, bot navigation and behaviors looked mechanical and bot-like in multiplayer lobbies:
1. **Synchronized Market Conveyor Belt**: When entering or respawning in Harbor, `harborTicks == 1` forced *every* bot to sail directly to Market dock `(9, -1)` before doing anything else. With ~200 bots on the server, this produced an obvious, single-file automated procession regardless of whether bots had coins or upgrades.
2. **Euclidean Straight-Line Sailing**: Direct vectors with zero rudder adjustment, zero ocean sway, and identical floating-point coordinates made sailing look artificial.
3. **Instantaneous 1-Frame Aim Snaps**: In combat, crosshairs snapped mathematically to target centers with infinite angular acceleration, standing out on spectator views and server logs.
4. **Synchronized Manager Tickers**: All bots ran on a rigid `3 * time.Second` ticker, creating synchronous burst patterns.

### 2. Architectural Implementation
In `/home/agent/orca/learnings/reefblast-go/daemon.go`:

1. **Need-Based Harbor Decision Tree & Natural Fleet Distribution**:
   - Replaced rigid tick-based routing with an intelligent need-based decision engine in `manageHarbor()`:
     - **Boss Rush Challenge**: Checks `checkNeedsIslandRun()` $\to$ sails to Boss Rush islet `(-130.9, -102.3)` only if daily quest pending.
     - **Shipyard**: `checkHullUpgrade()` $\to$ sails directly to `(26, -6.5)` only if an affordable superior vessel hull is ready.
     - **Forge**: `checkForgeCrafting()` $\to$ sails directly to `(-26, -6.5)` only if a superior craftable weapon is ready.
     - **Tavern**: `checkHelperRecruitment()` $\to$ sails directly to `(-9, -1.0)` only if a superior crew helper is affordable.
     - **Market**: `checkMarketSelling()` $\to$ sails to `(9, -1.0)` only when listing surplus items ($\ge 5$ items).
     - **Direct Gate Transit (~75% of voyages)**: Bots without pending business sail DIRECTLY to their level-appropriate Zone Gate. Out of 200 bots, ~150 are distributed across all 20 zone gates across the map rather than clumping at dock `(9, -1)`.

2. **Deterministic Player Archetypes / Personas**:
   - Seeded from `botName` hash:
     - **Grinder (65%)**: Pure combat focus; heads straight to zone gates, only visits docks when upgrades are immediately actionable.
     - **Trader (20%)**: Browses market listings periodically (~8–12 mins), actively sells surplus gear.
     - **Explorer (15%)**: Occasionally (~10% chance) takes a brief scenic harbor canal sweep past the channel before departing.

3. **Human Pacing, Dock Dwell Pauses & Desynchronized Tickers**:
   - **Desynchronized Manager Loop**: Replaced fixed 3s sleep with seeded PRNG jitter (`2200ms + rand(1600ms)`), ensuring 200 bots never query or act in lockstep.
   - **Dock Dwell Delays**: When interacting at Shipyard, Forge, Tavern, or Market, bots pause for `1.2s - 2.5s`, simulating human reading/purchasing delay.
   - **Gate Departure Hesitation**: Bots pause naturally for `400ms - 1000ms` at gate portals before executing zone transit.

4. **Multi-Harmonic Ocean Wave Sway & Naval Hydrodynamics**:
   - Lateral rudder oscillation perpendicular to heading:
     $$\vec{i}' = \vec{u} + \text{sway} \cdot \vec{u}_{\perp}, \quad \text{sway} = A \cdot \left[0.75 \sin(\omega t + \phi) + 0.25 \sin(2.3\omega t + 1.4\phi)\right]$$
   - Seeded wave frequency ($\omega \approx 2.0\text{ rad/s}$) and phase ($\phi$) ensure no two vessels oscillate in sync.
   - Steering vectors strictly normalized ($\|\vec{i}\| = 1.0$), guaranteeing velocity $\le \text{baseSpeed}$ to prevent server rubberband snapping.
   - Throttle surge ($96\% - 102\%$) and docking approach easing ($\max(0.42, \text{dist}/6.0)$).
   - Long voyages ($>38$ units) follow gentle transverse arcs ($\pm 3.5$ units).
   - Dock stop points apply deterministic $\pm 0.85$ X and $\pm 0.50$ Z jitter so vessels park side-by-side.
   - Zero-allocation `sendMvRaw` serialization drops 10 Hz movement heap allocation to **0 B/op**.

5. **Human Combat Aim Smoothing & Hand Micro-Tremor**:
   - Implemented exponential aim smoothing (`alpha = 0.58`) with fast human flick on target switch (`alpha = 0.82` when $\Delta d > 6.0$).
   - Subtle hand micro-tremor ($\pm 0.12$ units) around target centers:
     $$\text{tremorX} = 0.12 \cdot \sin(2.7t + \text{seed}), \quad \text{tremorZ} = 0.12 \cdot \cos(3.1t + 1.4\text{seed})$$
   - Tremor stays 100% inside fish hitboxes (radii $0.8$–$2.5$), preserving combos and hit rates while looking completely organic.
   - **Emergency Defense Bypass**: Doom Orb and lethal torpedo interceptions execute with `emergency = true` (instant 100% snap), guaranteeing flawless defensive survivability.

### 3. Live Production Validation
- **Cuang-Bot (Lv 104, Void Reaver)**: Transits directly to Zone 19 Gate with natural wave sway; memory stable at **6.05 MiB**, CPU **0.41%**.
- **Captain William (Lv 131, x-warlord)**: Transits directly to Zone 19 Gate; memory stable at **5.93 MiB**, CPU **0.37%**.
- **Fleet Verification**: Inspected `capt_cd40e6` (Zone 6) and `capt_1a74cb` (Zone 8); all bots independently sail to their respective gates without artificial market detours.
- **Unit Test Suite**: 6 tests passing in 0.010s (`sailing_test.go` and `snapshot_test.go`).

---

## 13. Phase 17: Hybrid Tier Swarm Architecture & High-Performance Go Engine

### 1. Problem Statement
As the fleet expanded toward 200 bots, two critical bottlenecks emerged:
1. **Host Container Overhead**: Running 200 bots as individual Docker containers would consume ~2.3 GiB of host RAM just for containerd-shims (~3.5 MiB each $\times 200 \approx 700\text{ MiB}$), kernel veth network interfaces (~200 MiB), and duplicated Go runtimes (~1.2 GiB).
2. **Discord Bot Telemetry Latency**: Querying dozens of individual Docker containers via Docker Engine API (`/containers/.../archive`) caused multi-second lag spikes when loading Discord profiles or executing mass commands.
3. **Journald Logging Failure**: Docker daemon on `docker-host` defaults to the `journald` log driver, which threw fatal errors (`docker: Error response from daemon: unknown log opt 'max-file' for journald log driver`) during `/reefblast add` account provisioning.

### 2. The Hybrid Tier Architecture
To balance VIP account isolation with ultra-low resource scaling:
- **Tier 1 (VIP / Core Accounts)**:
  - **Cuang-Bot (`reefblast-bot`, Lv 106)** and **Captain William (`reefblast-bot-william`, Lv 132)** remain in dedicated, isolated Docker containers.
  - Guaranteed dedicated CPU core/priority and zero risk of interference from farming accounts.
- **Tier 2 (Autonomous Go Swarm Engine - `reefblast-swarm`)**:
  - Accounts `#3` through `#200+` run inside a single multi-bot container (`reefblast-swarm`).
  - Implemented in Go using lightweight goroutines (~150 KiB heap per bot) sharing a single in-memory copy of `GameDefs` and Go runtime.
  - Managed by a robust supervisor with panic `recover()` wrappers ensuring a crash in one bot goroutine cannot disrupt the fleet.
  - Embedded high-speed internal HTTP API on port `8989` providing instant in-memory telemetry and control.

### 3. Key Components Implemented

#### A. Go Swarm Engine (`reefblast-go/swarm.go`)
- **`SwarmManager`**: Thread-safe manager handling bot lifecycles, goroutine supervisors, and configuration.
- **Persistent Fleet Config (`fleet.json`)**: Atomic JSON storage (`.tmp` write + `os.Rename`) preserving account tokens, credentials, and policies across container restarts.
- **Embedded HTTP REST API (Port 8989)**:
  - `GET /health`: Engine status, total bots, and running count.
  - `GET /fleet`: Full in-memory snapshot of all bots with full AM-GM skills, levels, coins, zones, and combos in **<2ms**.
  - `GET /bot?key=<botKey>`: Individual bot telemetry dump.
  - `POST /fleet/add`: Instant dynamic bot provisioning (single or batch JSON).
  - `POST /fleet/start`: Single or batch bot resume.
  - `POST /fleet/stop`: Single or batch bot pause.
  - `POST /fleet/policy`: Runtime policy updates (buying, salvaging, selling).
  - `DELETE /fleet/remove`: Decommissions account from config and shuts down its goroutine.

#### B. Dual-Mode Binary (`reefblast-go/main.go` & `daemon.go`)
- Detection via `-swarm` flag or `RB_SWARM_MODE=true`:
  - **Swarm Mode**: `GOMAXPROCS=2`, `GOMEMLIMIT=128MiB`, `GOGC=50`, listens on HTTP port 8989.
  - **Single-Bot Mode**: `GOMAXPROCS=1`, `GOMEMLIMIT=16MiB`, `GOGC=50` (lean isolated runner for Cuang/William).
- Added `Stop()` method in `daemon.go` with `stopChan` for clean WebSocket teardown and goroutine termination.
- Exported thread-safe `GetStatus() StatusDump` for in-memory status serving without disk I/O.

#### C. Discord Bot Integration (`AI-nimate/scripts/discord-bot.js`)
- **Swarm Client**: Added `swarmRequest()` with multi-host automatic fallback (`http://reefblast-swarm:8989` $\to$ `http://10.20.20.11:8989` $\to$ `http://127.0.0.1:8989`).
- **Journald Fix**: Prepend `--log-driver json-file` on any fallback container creation.
- **Sub-Millisecond Telemetry**: `fetchAllBotStatuses()` fetches Swarm fleet in <2ms and merges with isolated Core containers `#1` and `#2`.
- **Batch Command Acceleration**: `startBotContainers` and `stopBotContainers` route swarm targets through single HTTP batch POST calls instead of slow sequential Docker CLI commands.
- **Discord Embed 1024-Char Guard**: Compacted memory formatting in monospace roster and enforced strict field string truncation to satisfy discord.js constraints.

### 4. Resource Benchmark & Verification

| Architecture | 10 Bots RAM | 200 Bots RAM (Projected) | Host Network Interfaces | Telemetry Latency |
| :--- | :--- | :--- | :--- | :--- |
| **Standalone Containers** | ~115 MiB | ~2,300 MiB (2.3 GiB) | 200 `veth` pairs | 2,500ms – 5,000ms |
| **Hybrid Swarm Engine** | **7.95 MiB** | **~50 – 65 MiB** | 1 `veth` interface | **< 20ms** |
| **Efficiency Gain** | **> 93% Reduction** | **> 97% Reduction** | **99.5% Reduction** | **250x Faster** |

#### Live Container Metrics (`docker stats --no-stream`):
- `reefblast-bot` (Cuang, Lv 106): **9.15 MiB RAM**, CPU 0.00%
- `reefblast-bot-william` (William, Lv 132): **9.23 MiB RAM**, CPU 0.54%
- `reefblast-swarm` (10 Farming Captains): **7.95 MiB RAM**, CPU 4.01%
- `cuangbot` (Discord Bot): **48.64 MiB RAM**, CPU 0.00%
- Host Containers: Reduced from 36 down to 26; 0 unhealthy containers.
- All 12 bots verified online, farming, and responsive via Discord commands and HTTP APIs.

---

## 14. Phase 18: One-Off Target Weapon Exception Engine (Plague Storm Targeting)

### 1. Problem Statement & Objective
- **Objective**: Target the pinnacle weapon in Reefblast (**Plague Storm** / `x_plague`) specifically for Captain William (`reefblast-bot-william`), bypassing normal greedy incremental upgrades.
- **Strict Constraints**:
  1. William must hoard coins without prematurely spending them on intermediate Tier 7 crafts (e.g. Hexfire Coil `x_hexcoil` for 29.7M, Wraith Lance `x_wraithlance` for 31.5M, etc.).
  2. Combat in Zone 19 must not be interrupted for premature Harbor trips while saving for Plague Storm (37,300,000 coins).
  3. Once $\ge 37,300,000$ coins are accumulated, William must automatically sail to the Forge, craft `x_plague`, and equip it.
  4. **One-Off Guarantee**: Upon acquiring `x_plague`, the target weapon directive must self-clear from in-memory state and `policy.json` (`targetWeapon = ""`), returning William to normal autonomous operation so the exception is **never repeated**.
  5. Discord bot UI and telemetry must display the target weapon status cleanly.

### 2. Architectural Implementation
1. **Policy Schema Extension (`types.go` & `daemon.go`)**:
   - Added `TargetWeapon string `json:"targetWeapon,omitempty"`` to `Policy` struct.
   - Initialized from `policy.json` or `RB_TARGET_WEAPON` environment variable fallback.
   - Added `savePolicyLocked()` atomic serializer (`policy.json.tmp` $\to$ `os.Rename`).
   - If `targetWeapon` is already present in player inventory on startup, `targetWeapon` is automatically cleared.

2. **Harbor Voyage & Forge Targeting Logic (`daemon.go`)**:
   - **`checkNeedsHarborTrip()`**: If `targetWeapon` is configured and coins $< 37.3\text{M}$, blocks premature harbor trips for lower-tier crafts, keeping William farming in Zone 19 uninterrupted.
   - **`checkForgeCrafting()`**:
     - Hoards coins strictly for `targetWeapon`.
     - When coins $\ge 37.3\text{M}$, routes directly to Forge `(-26.0, -6.5)`, submits `craft` packet for `targetWeapon`, verifies acquisition, equips the weapon, and immediately clears `targetWeapon = ""` in memory and writes updated `policy.json`.
   - **`handleMarketList()`**: Prevents spending coins on non-target market weapons while hoarding.

3. **Discord Bot Telemetry Integration (`AI-nimate/scripts/discord-bot.js`)**:
   - Added Tier 6 & Tier 7 weapon names (`x_plague` $\to$ `Plague Storm`, `x_hexcoil` $\to$ `Hexfire Coil`, etc.).
   - Added active target weapon field to `/reefblast profile target:william` telemetry card:
     `• **Target Weapon**: Plague Storm (x_plague) 🎯 *(One-off priority exception)*`.
   - Hardened `deployBotDaemon()` cache invalidation against `ReferenceError`.

### 3. Unit Test Verification
- Implemented `TestTargetWeaponOneOffException` in `reefblast-go/swarm_test.go`:
  - Validates rejection of `x_hexcoil` at 29.7M coins.
  - Validates forging of `x_plague` at 37.5M coins.
  - Validates automatic clearing of `targetWeapon` in memory and disk `policy.json`.
  - Result: `PASS` (5.42s).

### 4. Live Production Status
- **Captain William (`reefblast-bot-william`)**:
  - **Plague Storm (`x_plague`) Successfully Acquired & Equipped**:
    - Rolls: `+7.4% Atk`, `+20.4% Aspd`, `+9.1% Crit` (Base DPS: 13,846).
    - Directive automatically self-cleared from memory and `policy.json`.
  - **Level**: `135` (Level requirement for Zone 19 is 103).
  - **Zone Clarification**:
    - Reefblast defines **20 combat zones total**, but they are **0-indexed** in the server protocol and game definitions (`0` through `19`).
    - `Zone 0` is the 1st zone ("Coral Reef", Lv 1).
    - `Zone 19` is the **20th and highest zone in the game** ("Leviathan's Throne", Lv 103).
    - There is no "Zone 20" index on the server (`defs.zones[20]` is undefined). William is already in the absolute highest combat zone in the game.
    - Updated Discord bot telemetry formatting to display: `Zone 19: Leviathan's Throne 👑 [Max Zone 20/20]`.

---

## 15. Phase 19: Weapon Quality Percentage Engine (In Logs & Profiles)

### 1. Objective & Formula
- **Client Quality Formula**:
  $$\text{bar}(v, r) = \frac{v - r_{\min}}{r_{\max} - r_{\min}}$$
  $$\text{Quality}(it) = \text{round}\left(\frac{\text{bar}(atk) + \text{bar}(aspd) + \text{bar}(crit)}{3} \times 100\right)$$
  - ATK roll bounds: `[-0.20, +0.50]`
  - ASPD roll bounds: `[-0.20, +0.50]`
  - CRIT roll bounds: `[-0.05, +0.15]`
  - Gold threshold: $\ge 70\%$ | Red threshold: $< 40\%$

### 2. Implementation Overview
1. **Go Daemon Schema & Calculations (`types.go` & `daemon.go`)**:
   - Added `RollDef` and `Rolls []RollDef` to `GameDefs`.
   - Added `Quality int `json:"quality,omitempty"`` to `Item`.
   - Implemented `CalcItemQuality(it *Item) int` matching the official client formula.
   - Implemented `LogWeaponArsenal()` outputting a structured quality audit for all owned weapons.
   - Updated `autoEquipBestWeapon()` and `case "me"` to log weapon quality on equipment changes and acquisitions.
   - `GetStatus()` serializes `quality` into `status.json`.
2. **Discord Bot Profile & Telemetry (`AI-nimate/scripts/discord-bot.js`)**:
   - Implemented `calcItemQuality(it)` helper.
   - Added equipped weapon quality badge in `🚢 Vessel & Fleet`:
     `• ⚔️ **Weapon**: Plague Storm [Qual: **56%**]`
   - Added individual weapon quality badges in `🎒 Weapon Arsenal`:
     `9. **Plague Storm** 🎯 [EQUIPPED] [Quality: **56%**] (+7.4% Atk, +20.4% Spd, +9.1% Crit)`
   - Added quality tag to fleet embed cards (`[56% Qual]`).
3. **Unit Tests & Verification**:
   - Added `TestCalcItemQuality` in `reefblast-go/swarm_test.go` verifying:
     - Starter bound popper $\to$ `0%`
     - William's Plague Storm $\to$ `56%`
     - Perfect god roll $\to$ `100%`
     - Minimum roll $\to$ `0%`
   - All tests pass (`ok reefblast-go 5.227s`).
   - Deployed updated binary and Discord bot across all containers (`reefblast-bot-william`, `reefblast-bot`, `reefblast-swarm`, `cuangbot`).

---

## 16. Phase 20: Resilient Multi-Container Account Provisioning & CF WARP Egress Routing

### 1. Root Causes of Provisioning Failures Identified & Solved
1. **Discord API Form Body Size Overflow (`MAX_EMBED_SIZE_EXCEEDED: Embed size exceeds maximum size of 6000`)**:
   - The Discord bot was rendering monospace rosters in unbounded chunks. At 92+ bots, the embed size exceeded 6,500 characters, throwing Discord API error 50035 and crashing the Node.js process due to missing client error listeners.
   - **Fix**: Added `sanitizeEmbed(embed)` to enforce strict 4,800 character ceilings and 20 fields max. Restructured fleet telemetry to display summary analytics, shard distribution, Core Bots, and a clean top-12 spotlight table.
2. **Missing Process & Client Error Listeners**:
   - Node.js v24 terminated on any unhandled rejection or Discord REST error.
   - **Fix**: Added top-level `process.on('unhandledRejection')`, `process.on('uncaughtException')`, `client.on('error')`, and `client.on('shardError')`.
3. **Discord Interaction 15-Minute TTL Expiration (`10062: Unknown interaction`)**:
   - Synchronous sequential loops for large batches (up to 1,000 accounts) exceeded Discord's 15-minute token limit.
   - **Fix**: Built `runProvisionJob` with throttled status updates (every 3 seconds), live visual progress bars, and an automatic channel-detach mechanism at 13 minutes to ensure long jobs never fail.
4. **Go Swarm Config Permission Denial in Container**:
   - `reefblast-swarm` container directory `/app` was owned by `root`, preventing `pwuser` from writing `fleet.json`.
   - **Fix**: Set `chown -R pwuser:pwuser /app` and synced `fleet.json`.

### 2. Multi-Container Swarm Sharding Architecture
- **Sharding Strategy**: Rather than a monolithic single container or 1 container per bot, bots are partitioned into multi-bot swarm shards (up to 50 bots per container):
  - **Shard 1 (`reefblast-swarm`, port 8989)**: Initial 90 bots.
  - **Shard 2 (`reefblast-swarm-2`, port 8990)**: Active and healthy, hosting newly provisioned accounts.
  - **Dynamic Shard Scaling**: `SwarmClusterManager` auto-detects capacity and launches `reefblast-swarm-${N}` on host ports `8990..8995` via Docker API when needed.
- **Unified Fleet Telemetry**: `fetchAllBotStatuses()` queries all active swarm shards concurrently via `/fleet` and merges the cluster into a single cohesive telemetry view with shard attribution.

### 3. Cloudflare WARP Egress Routing & Anti-Block Engine
- **Rotating Network Routes**:
  - `direct`: Host IP (`124.153.16.26`)
  - `cf`: Cloudflare WARP SOCKS5 proxy (`socks5h://10.20.20.11:1080`) egressing from Cloudflare IP (`2a09:bac1:...`).
  - `auto`: Round-robin / 50-50 load-balanced rotation between direct and CF WARP proxy.
- **Realistic Browser Fingerprinting**: Randomizes modern user-agents (Chrome 129, Edge 129, Safari 17, Firefox 131) and includes authentic `sec-ch-ua`, `Origin`, and `Referer` headers.
- **Failover**: If a registration encounters HTTP 429 (rate-limit) or 403 (WAF challenge), it automatically fails over to the alternate route with exponential backoff.

### 4. Interactive Progress Bar & Status Command
- **Visual Bar**: `[████████░░░░░░░░] 50% (50/100)` with live speed (`bots/s`), elapsed time, ETA, active shard, and current route.
- **Slash Commands**:
  - `/reefblast add [mode] [count] [buying] [route: auto|cf|direct]`: Triggers resilient provisioning with live visual feedback.
  - `/reefblast progress`: Live status tracker for the active or most recent bulk provisioning job.
  - Interactive `🔄 Refresh Progress` button on live status embeds.

---

## 17. Phase 21: Fleet Roster Scrolling & Provision Job Cancellation

### 1. Account Provisioning Status & Cancellation
- **Job Status & Cancellation**:
  - The previous bulk provisioning job was cancelled cleanly and will no longer attempt registrations in the background.
  - Added `/reefblast cancel` slash command to cancel any active or queued bulk provisioning jobs immediately.
  - Added an interactive `🛑 Cancel` button on the live progress embeds (`rb:btn:job_cancel:${jobId}`).
  - Provisioning loops (`provisionRandomBots`, `provisionEmailList`) now check `options.isCancelled()` on each step, allowing instantaneous termination.
- **Current Cluster Status (184 Total Daemons)**:
  - **Shard 1 (`reefblast-swarm`, port 8989)**: 90 bots (Lv. 10–67).
  - **Shard 2 (`reefblast-swarm-2`, port 8990)**: 50 bots (Lv. 2–14).
  - **Shard 3 (`reefblast-swarm-3`, port 8991)**: 42 bots (Lv. 2–12).
  - **Core Bots**: `cuang` (Lv. 32) + `william` (Lv. 135, Plague Storm equipped).
  - All 182 swarm bots + Cuang are 100% active, farming, leveling, and claiming quests.

### 2. Interactive Fleet Roster Scrolling & Pagination
- **Multi-Page Roster Table (15 Bots per Page)**:
  - Structured monospace roster display:
    ```text
     # | St | Captain        | Level | Zone     | Shard
    ---+----+----------------+-------+----------+---------
    ```
  - Total embed payload is clamped to ~1.8KB, ensuring 100% immunity to Discord's 6,000-character limit.
- **Scroll & Navigation Controls (Row 0)**:
  - `⏮️ First`: Jumps to page 1 (`rb:fleet_nav:first:0`, auto-disabled on page 1).
  - `◀️ Prev`: Navigates to previous page (`rb:fleet_nav:prev:${prevPage}`, auto-disabled on page 1).
  - `📄 Page X / Y`: Opens an interactive Discord modal (`rb:modal:fleet_jump`) allowing direct page jumping (e.g. typing `4` to jump to page 4).
  - `Next ▶️`: Navigates to next page (`rb:fleet_nav:next:${nextPage}`, auto-disabled on last page).
  - `⏭️ Last`: Jumps to the final page (`rb:fleet_nav:last:${totalPages-1}`, auto-disabled on last page).
- **Dynamic Contextual Dropdown Menu (Row 3)**:
  - `buildProfileSelectRow`: Dynamically renders the 15 bots on the currently viewed page.
  - Displays bot index, name, level, shard, zone, coins, and active/disabled status emoji.
  - In-dropdown pagination: `⬆️ Browse Previous Page`, `⬇️ Browse Next Page`, and `🔢 Jump to Any Bot # or Page...`.
  - Selecting any bot opens their full Captain Profile card.
- **Bidirectional Fleet Navigation**:
  - From any Captain Profile, clicking `🌐 Fleet View` returns directly to the exact page containing that bot.
- **Global Slash Command Updates**:
  - `/reefblast status [page: 1..N]`: Directly open any page of the roster.
  - `/reefblast cancel`: Terminate active bulk provisioning jobs.

---

## 18. Phase 22: Resolving Next/Prev Interaction Failures & Component Hardening

### 1. Root Cause Analysis: `next/prev` Interaction Failure
- **Diagnosis**:
  - Whenever users clicked `Next ▶️` from Page 0 to Page 1, Discord threw:
    `[safeEditReply warn]: Invalid Form Body: components[0].components[4].custom_id[COMPONENT_CUSTOM_ID_DUPLICATED]: Component custom id cannot be duplicated`
  - On Page 1 (`safePage = 1`):
    - Component 0 (`⏮️ First`): `customId = 'rb:fleet_page:0'`
    - Component 1 (`◀️ Prev`): `customId = 'rb:fleet_page:0'` (`1 - 1 = 0`)
    - Identical custom IDs in the same message violated Discord API validation constraints.
  - On Page `totalPages - 2`:
    - Component 3 (`Next ▶️`) and Component 4 (`⏭️ Last`) both had `'rb:fleet_page:${totalPages-1}'`.
  - On single-page rosters (`totalPages = 1`):
    - Component 0 and Component 4 both had `'rb:fleet_page:0'`.
  - In Captain Profiles:
    - If a fleet had $\le 2$ bots, `prevKey === nextKey`, duplicating `rb:profile:${prevKey}` on both `◀️` and `▶️`.

### 2. Permanent Architectural Fix
- **Distinct Semantic Custom IDs**:
  - Scroll Row:
    - `rb:fleet_nav:first:0`
    - `rb:fleet_nav:prev:${Math.max(0, safePage - 1)}`
    - `rb:open_modal:fleet_jump:${safePage + 1}:${totalPages}`
    - `rb:fleet_nav:next:${Math.min(totalPages - 1, safePage + 1)}`
    - `rb:fleet_nav:last:${totalPages - 1}`
  - Captain Profile Nav:
    - `rb:profile:prev:${prevKey}`
    - `rb:profile:next:${nextKey}`
  - Guaranteed 100% globally unique custom IDs across all components regardless of page number or fleet size.
- **Defensive Component Sanitization (`sanitizeComponents`)**:
  - Added automatic deduplication and validation layer in `safeEditReply()`:
    - Inspects all ActionRows before submission to Discord.
    - If an identical `custom_id` is ever detected, automatically appends a random nonce suffix (`_NNN`).
    - Enforces strict 100-character truncation for all select menu options, labels, descriptions, and values.
- **Resilient Interaction Lifecycle**:
  - Wrapped all `interaction.deferUpdate()` and `interaction.deferReply()` invocations with `.catch()` to avoid unhandled rejections if Discord gateway delivers late callbacks.
  - Switched all button and modal reply paths from raw `interaction.editReply()` to `safeEditReply()`.
  - Re-routed profile card navigation to use fast 4-second cached status queries (`fetchAllBotStatuses(false)`).

### 3. Resource & Memory Telemetry (Current Active Fleet: 242 Bots)
- **Host System (4.0 GiB RAM)**:
  - Total Memory: `3.8 GiB`
  - Used: `2.0 GiB` (52%)
  - Free/Buffer: `1.8 GiB` available
  - Swap: `9.8 GiB` free
- **Swarm Shards**:
  - `reefblast-swarm` (90 bots): `17.85 MiB` RSS
  - `reefblast-swarm-2` (50 bots): `5.96 MiB` RSS
  - `reefblast-swarm-3` (50 bots): `12.89 MiB` RSS
  - `reefblast-swarm-4` (50 bots): `12.66 MiB` RSS
  - **Total Swarm Footprint**: **~49.36 MiB** (~0.20 MiB per bot daemon)
- **Controllers & Support**:
  - `cuangbot` (Discord Bot Gateway): `43.05 MiB` RSS
  - `interesting_shamir` (Reefblast Core Bot): `9.90 MiB` RSS
  - `idx_warp_proxy` (Cloudflare WARP Proxy): `281 MiB`









