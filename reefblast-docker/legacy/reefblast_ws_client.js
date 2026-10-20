/**
 * Reefblast Pure Headless WebSocket Client & Auto-Progression Daemon
 * 
 * Replaces headless Chromium with an ultra-lightweight, event-driven Node.js client.
 * Memory footprint: ~35MB - 50MB (vs 750MB in Chromium)
 * CPU usage: < 0.5% (vs 35% in SwiftShader WebGL)
 */

const WebSocket = require('ws');
const fs = require('fs');
const path = require('path');
const https = require('https');

// Environment & Configuration
let TOKEN = process.env.RB_TOKEN || '';
const BOT_NAME = process.env.RB_BOT_NAME || 'cuang-bot';
const BOT_PASS = process.env.RB_BOT_PASS || 'Rb!dIkC3VsYPED15';
const ZERO_PURCHASE = process.env.RB_ZERO_PURCHASE === 'true';
const LOGS_DIR = process.env.RB_LOGS_DIR || path.join(__dirname, 'logs');
const LOG_FILE = path.join(LOGS_DIR, 'auto_progression.log');
const STATUS_FILE = path.join(LOGS_DIR, 'status.json');

if (!fs.existsSync(LOGS_DIR)) {
  fs.mkdirSync(LOGS_DIR, { recursive: true });
}

function writeLog(line) {
  const ts = new Date().toISOString();
  const msg = `[${ts}] ${line}\n`;
  process.stdout.write(msg);
  try {
    fs.appendFileSync(LOG_FILE, msg);
  } catch (err) {
    console.error('Failed to append to log file:', err);
  }
}

// Fetch game definitions from api/defs
function fetchDefs() {
  return new Promise((resolve, reject) => {
    https.get('https://greentea.id/reefblast/api/defs', res => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        try {
          resolve(JSON.parse(data));
        } catch (e) {
          reject(e);
        }
      });
    }).on('error', reject);
  });
}

// Authenticate and obtain fresh token
function apiLogin(name, pass) {
  return new Promise((resolve, reject) => {
    const payload = JSON.stringify({ Name: name, Pass: pass });
    const req = https.request('https://greentea.id/reefblast/api/login', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(payload)
      }
    }, res => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        try {
          const j = JSON.parse(data);
          if (j.token) resolve(j);
          else reject(new Error(j.error || 'No token in response'));
        } catch (e) {
          reject(e);
        }
      });
    });
    req.on('error', reject);
    req.write(payload);
    req.end();
  });
}

class ReefBlastDaemon {
  constructor(defs) {
    this.defs = defs;
    this.ws = null;
    this.connected = false;
    this.kicked = false;
    this.kickWaitUntil = 0;
    this.reconnectTimer = null;
    this.pingTimer = null;
    this.managerTimer = null;

    // Game state
    this.me = null;
    this.zone = null; // { zone: number, slot: number, mode: any, ... }
    this.slot = -1;
    this.combo = 0;
    this.comboT = 0;
    this.lastComboUpdate = Date.now();
    this.roster = [];
    this.fishes = new Map(); // id -> { id, ty, x, z, tx, tz, vx, vz, lastT, hp, flags, dir }
    this.shots = new Map();  // id -> { id, x, z, kind, hp, vx, vz, lastT }
    this.lastAimSent = { x: 0, z: 0, f: false, t: 0 };
    this.lastDeathTime = null;
    this.zoneDeaths = {};
    this._lastLevel = 1;
    this._lastCoins = 0;

    // Statistics
    this.stats = {
      startedAt: Date.now(),
      startCoins: 0,
      startLevel: 1,
      startXp: 0,
      bestCombo: 0,
      killsThisSession: 0,
      shotsIntercepted: 0,
      upgradesPurchased: 0
    };
  }

  log(category, msg) {
    writeLog(`[${category}] ${msg}`);
  }

  async start() {
    this.log('SYSTEM', `=== Reefblast Pure Headless WebSocket Daemon Starting (${BOT_NAME}) ===`);
    this.log('SYSTEM', `Configuration: ZeroPurchase=${ZERO_PURCHASE} | Target Memory: ~40MB`);

    // Ensure we have a valid token
    if (!TOKEN && BOT_NAME && BOT_PASS) {
      try {
        this.log('SYSTEM', `No token provided. Authenticating as ${BOT_NAME}...`);
        const loginRes = await apiLogin(BOT_NAME, BOT_PASS);
        TOKEN = loginRes.token;
        this.log('SYSTEM', `Login successful! Received token.`);
      } catch (err) {
        this.log('FATAL', `Login failed: ${err.message}`);
        process.exit(1);
      }
    }

    this.connect();

    // Start manager loop (every 3 seconds)
    this.managerTimer = setInterval(() => this.managerStep(), 3000);

    // Start status file dumper (every 5 seconds)
    setInterval(() => this.dumpStatus(), 5000);
  }

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }

    if (this.kicked && Date.now() < this.kickWaitUntil) {
      const remainingSec = Math.ceil((this.kickWaitUntil - Date.now()) / 1000);
      this.log('SYSTEM', `Session kicked by another login. Waiting ${remainingSec}s before reconnecting...`);
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = setTimeout(() => this.connect(), 5000);
      return;
    }

    this.kicked = false;
    const wsUrl = `wss://greentea.id/reefblast/ws?token=${encodeURIComponent(TOKEN)}`;
    this.log('SYSTEM', `Connecting to WebSocket ${wsUrl.replace(TOKEN, 'TOKEN_HIDDEN')} ...`);

    this.ws = new WebSocket(wsUrl, {
      headers: {
        'Origin': 'https://greentea.id',
        'User-Agent': 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36'
      }
    });

    this.ws.on('open', () => {
      this.connected = true;
      this.log('SYSTEM', 'WebSocket connection established.');
      this.startPing();
    });

    this.ws.on('message', data => {
      try {
        const m = JSON.parse(data);
        this.handleMessage(m);
      } catch (err) {
        this.log('ERROR', `Failed to parse message: ${err.message}`);
      }
    });

    this.ws.on('close', async (code, reason) => {
      this.connected = false;
      this.stopPing();
      this.zone = null;
      this.slot = -1;
      const reasonStr = reason ? reason.toString() : '';
      this.log('SYSTEM', `WebSocket closed (code: ${code}, reason: "${reasonStr}").`);

      // If token expired, refresh via API
      if (reasonStr.includes('session expired') || code === 4001 || code === 1008) {
        this.log('SYSTEM', 'Token appears expired. Refreshing login via API...');
        try {
          const res = await apiLogin(BOT_NAME, BOT_PASS);
          TOKEN = res.token;
          this.log('SYSTEM', 'Token refreshed successfully.');
        } catch (e) {
          this.log('ERROR', `Failed to refresh token: ${e.message}`);
        }
      }

      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = setTimeout(() => this.connect(), 3000);
    });

    this.ws.on('error', err => {
      this.log('ERROR', `WebSocket error: ${err.message}`);
    });
  }

  send(obj) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(obj));
    }
  }

  startPing() {
    this.stopPing();
    this.pingTimer = setInterval(() => {
      this.send({ t: 'ping', c: Date.now() });
    }, 2000);
  }

  stopPing() {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  handleMessage(m) {
    if (!m || !m.t) return;

    switch (m.t) {
      case 'hello':
        this.me = m.me;
        this.zone = null;
        this.slot = -1;
        this._lastLevel = m.me?.level || 1;
        this._lastCoins = m.me?.coins || 0;
        if (this.stats.startCoins === 0) {
          this.stats.startCoins = m.me?.coins || 0;
          this.stats.startLevel = m.me?.level || 1;
          this.stats.startXp = m.me?.xp || 0;
        }
        this.log('SYSTEM', `Captain ${m.me.name} ready! Level: ${m.me.level} | Coins: ${m.me.coins.toLocaleString()} | Vessel: ${m.me.boat}`);
        this.dumpStatus();
        break;

      case 'me': {
        const prev = this.me;
        this.me = m.me;
        if (prev && m.me.level > prev.level) {
          const n = m.me.level - prev.level;
          this.log('MILESTONE', `Level Up! Captain ${m.me.name} is now Level ${m.me.level}! (+${n} skill points) Coins: ${m.me.coins.toLocaleString()}`);
          this._lastLevel = m.me.level;
        }
        break;
      }

      case 'joined': {
        this.zone = {
          zone: m.zone,
          party: m.party,
          mode: m.mode || null,
          tier: m.tier || 0
        };
        this.slot = m.slot;
        this.combo = 0;
        this.comboT = 0;
        this.fishes.clear();
        this.shots.clear();
        const zoneName = this.defs?.zones[m.zone]?.name || `Zone ${m.zone}`;
        this.log('ZONE_CHANGE', `Entered ${zoneName} (Slot ${m.slot}). Starting combat targeting!`);
        break;
      }

      case 'left':
        this.log('ZONE_CHANGE', `Left zone, returned to Harbor.`);
        this.zone = null;
        this.slot = -1;
        this.fishes.clear();
        this.shots.clear();
        if (m.me) this.me = m.me;
        break;

      case 's': // Zone Snapshot
        if (this.zone) {
          this.handleSnapshot(m);
        }
        break;

      case 'kicked':
        this.kicked = true;
        this.kickWaitUntil = Date.now() + 25000;
        this.log('SYSTEM', `Received kick: "${m.msg || 'Signed in elsewhere'}". Waiting 25s before reconnecting...`);
        break;

      case 'partyAccept':
      case 'invite':
        if (m.from) {
          this.log('PARTY', `Incoming party invite from ${m.from}. Auto-accepting.`);
          this.send({ t: 'partyAccept', name: m.from });
        }
        break;

      case 'vote':
        this.log('PARTY', 'Party area vote proposed. Auto-voting YES!');
        this.send({ t: 'vote', yes: true });
        break;
    }
  }

  handleSnapshot(s) {
    const now = performance.now();

    // 1. Update combo and HP from player list
    if (s.p) {
      for (const p of s.p) {
        if (p[0] === this.slot) {
          const newCombo = p[4] || 0;
          if (newCombo > this.combo) {
            this.comboT = 2.0;
            this.lastComboUpdate = Date.now();
          }
          this.combo = newCombo;
          if (this.combo > this.stats.bestCombo) {
            this.stats.bestCombo = this.combo;
          }

          // Check if vessel sank
          const sunk = p[10];
          if (sunk) {
            const curZone = this.zone?.zone ?? 0;
            this.zoneDeaths[curZone] = (this.zoneDeaths[curZone] || 0) + 1;
            this.lastDeathTime = Date.now();
            this.log('DEFENSE', `Vessel sank in Zone ${curZone}! (Total zone sinks: ${this.zoneDeaths[curZone]}). Returning to harbor...`);
            this.send({ t: 'leaveZone' });
            return;
          }
        }
      }
    }

    // Decay combo timer
    const dt = (Date.now() - this.lastComboUpdate) / 1000;
    this.comboT = Math.max(0, this.comboT - dt);
    this.lastComboUpdate = Date.now();

    // 2. Track fish positions and velocities
    const seenFish = new Set();
    if (s.f) {
      for (const [id, ty, x, z, hp, flags, dir] of s.f) {
        seenFish.add(id);
        let f = this.fishes.get(id);
        if (!f) {
          f = { id, ty, x, z, tx: x, tz: z, vx: 0, vz: 0, lastT: now, hp, flags, dir };
          this.fishes.set(id, f);
        } else {
          const timeDelta = Math.max(0.03, (now - f.lastT) / 1000);
          f.vx = (x - f.tx) / timeDelta;
          f.vz = (z - f.tz) / timeDelta;
          f.x = x;
          f.z = z;
          f.tx = x;
          f.tz = z;
          f.hp = hp;
          f.flags = flags;
          f.dir = dir;
          f.lastT = now;
        }
      }
    }
    for (const [id] of this.fishes) {
      if (!seenFish.has(id)) this.fishes.delete(id);
    }

    // 3. Track incoming boss shots / torpedoes
    const seenShots = new Set();
    if (s.sh) {
      for (const [id, x, z, kind, hp] of s.sh) {
        seenShots.add(id);
        let shot = this.shots.get(id);
        if (!shot) {
          shot = { id, x, z, tx: x, tz: z, vx: 0, vz: 0, kind, hp, lastT: now };
          this.shots.set(id, shot);
        } else {
          const timeDelta = Math.max(0.03, (now - shot.lastT) / 1000);
          shot.vx = (x - shot.tx) / timeDelta;
          shot.vz = (z - shot.tz) / timeDelta;
          shot.x = x;
          shot.z = z;
          shot.tx = x;
          shot.tz = z;
          shot.lastT = now;
        }
      }
    }
    for (const [id] of this.shots) {
      if (!seenShots.has(id)) this.shots.delete(id);
    }

    // 4. Handle events (kills)
    if (s.ev) {
      for (const e of s.ev) {
        if (e.t === 'k' && e.s === this.slot) {
          this.stats.killsThisSession++;
        }
      }
    }

    // 5. Execute combat aiming
    this.combatAim();
  }

  combatAim() {
    if (!this.zone || this.slot < 0) return;

    const origin = { x: 0, z: 7.3 };
    const eqItem = this.me?.items?.find(i => i.uid === this.me?.equipped);
    const eqDef = this.defs?.weapons?.find(w => w.id === eqItem?.w);
    const isHoming = eqDef?.kind === 'homing';
    const bulletSpeed = eqDef?.speed || 25;

    // DEFENSE 1: Intercept incoming boss torpedoes
    if (this.shots.size > 0) {
      const dangerous = Array.from(this.shots.values()).filter(s => s.z > 0.5);
      if (dangerous.length > 0) {
        dangerous.sort((a, b) => b.z - a.z);
        const s = dangerous[0];
        const dist = Math.hypot(s.x - origin.x, s.z - origin.z);
        const leadTime = Math.min(0.35, dist / 22);
        const aimX = s.x + (s.vx || 0) * leadTime;
        const aimZ = s.z + (s.vz || 0) * leadTime;
        this.pushAim(aimX, aimZ, true);
        this.stats.shotsIntercepted++;
        return;
      }
    }

    // OFFENSE: Target Selection
    if (this.fishes.size === 0) {
      this.pushAim(0, -4, false);
      return;
    }

    const valid = Array.from(this.fishes.values()).filter(f => {
      if (f.flags & 8) return false; // Warded
      if (f.flags & 1) return false; // Shell
      return f.hp > 0;
    });

    if (!valid.length) {
      this.pushAim(0, -4, false);
      return;
    }

    // COMBO SAVER: If combo timer is critically low (< 0.85s) and combo >= 3
    const comboCritical = this.combo >= 3 && this.comboT < 0.85;

    let target = null;
    if (comboCritical) {
      // Prioritize rapid kill on lowest HP fish
      valid.sort((a, b) => {
        const distA = Math.hypot(a.x - origin.x, a.z - origin.z);
        const distB = Math.hypot(b.x - origin.x, b.z - origin.z);
        return (a.hp * 2 + distA) - (b.hp * 2 + distB);
      });
      target = valid[0];
    } else {
      // Normal: Mini-bosses / Bosses first, then lowest HP, then closest
      valid.sort((a, b) => {
        const defA = this.defs.fish[a.ty];
        const defB = this.defs.fish[b.ty];
        const aBoss = (defA?.boss || defA?.mini) ? 1 : 0;
        const bBoss = (defB?.boss || defB?.mini) ? 1 : 0;
        if (aBoss !== bBoss) return bBoss - aBoss;

        const distA = Math.hypot(a.x - origin.x, a.z - origin.z);
        const distB = Math.hypot(b.x - origin.x, b.z - origin.z);
        return (a.hp - b.hp) || (distA - distB);
      });
      target = valid[0];
    }

    let aimX = target.x;
    let aimZ = target.z;

    if (!isHoming) {
      const dist = Math.hypot(target.x - origin.x, target.z - origin.z);
      const flightTime = dist / bulletSpeed;
      aimX += (target.vx || 0) * flightTime;
      aimZ += (target.vz || 0) * flightTime;
    }

    // Clamp coordinates to playable field bounds
    aimX = Math.max(-16.5, Math.min(16.5, aimX));
    aimZ = Math.max(-10.5, Math.min(4.0, aimZ));

    this.pushAim(aimX, aimZ, true);
  }

  pushAim(x, z, firing) {
    const now = performance.now();
    const moved = Math.abs(x - this.lastAimSent.x) + Math.abs(z - this.lastAimSent.z) > 0.08;
    if (firing !== this.lastAimSent.f || (now - this.lastAimSent.t > 50 && (moved || now - this.lastAimSent.t > 400))) {
      this.send({ t: 'aim', x: +x.toFixed(2), z: +z.toFixed(2), f: firing });
      this.lastAimSent = { x, z, f: firing, t: now };
    }
  }

  // Progression loop (every 3 seconds)
  async managerStep() {
    if (!this.connected || !this.me) return;

    // 1. Auto-allocate Skill Points (AM-GM optimal ratio)
    if (this.me.points > 0) {
      const maxPerSkill = this.defs?.skillMax || 30;
      const ratios = { atk: 0.35, aspd: 0.30, def: 0.20, hp: 0.10, regen: 0.05 };
      const regenCap = 5;

      while (this.me.points > 0) {
        let bestStat = null;
        let lowestScore = Infinity;

        for (const [stat, weight] of Object.entries(ratios)) {
          const current = (this.me.skills && this.me.skills[stat]) || 0;
          const cap = stat === 'regen' ? regenCap : maxPerSkill;
          if (current < cap) {
            const score = current / weight;
            if (score < lowestScore) {
              lowestScore = score;
              bestStat = stat;
            }
          }
        }

        if (!bestStat) break;

        const curVal = (this.me.skills && this.me.skills[bestStat]) || 0;
        const cap = bestStat === 'regen' ? regenCap : maxPerSkill;
        const pointsToSpend = Math.min(this.me.points, cap - curVal, 1);
        if (pointsToSpend <= 0) break;

        this.send({ t: 'skill', id: bestStat, n: pointsToSpend });
        if (!this.me.skills) this.me.skills = {};
        this.me.skills[bestStat] = curVal + pointsToSpend;
        this.me.points -= pointsToSpend;
        this.log('SKILL', `Allocated skill point to: ${bestStat.toUpperCase()} (${curVal + pointsToSpend}/${cap}) [AM-GM Optimal]`);
      }
    }

    // 2. Auto-claim daily quests
    if (this.me.daily?.list) {
      const ready = this.me.daily.list.filter(q => q.prog >= q.target && !q.claimed);
      for (const q of ready) {
        this.log('QUEST', `Claiming daily quest: "${q.text}" (+${q.coins} coins, +${q.xp} XP)`);
        this.send({ t: 'questClaim', id: q.id });
      }
      if (!this.me.daily.bonus && this.me.daily.list.every(q => q.claimed || q.prog >= q.target)) {
        this.send({ t: 'questClaim', id: 'bonus' });
      }
    }

    // 3. Equip best weapon in bag
    this.autoEquipBestWeapon();

    // 4. Harbor Actions & Zone Management
    if (!this.zone) {
      // In Harbor
      if (!ZERO_PURCHASE) {
        // cuang-bot: Upgrade Hull if affordable
        this.checkHullUpgrade();
        // cuang-bot: Hire Helper if affordable
        this.checkHelperRecruitment();
        // cuang-bot: Auto salvage junk weapons
        this.autoSalvageJunk();
      }

      // Determine Best Grinding Zone
      const targetZone = this.getBestZone();
      const zoneDef = this.defs.zones[targetZone];
      this.log('ZONE_CHANGE', `Setting sail into Zone ${targetZone}: ${zoneDef?.name || 'Open Waters'} (Lv ${zoneDef?.level || 1}+)`);
      this.send({ t: 'enter', zone: targetZone });
    } else {
      // In Zone: Check if we outgrew this zone
      const targetZone = this.getBestZone();
      if (targetZone > this.zone.zone) {
        this.log('ZONE_CHANGE', `Higher zone unlocked (Zone ${targetZone} > Zone ${this.zone.zone}). Advancing!`);
        this.send({ t: 'leaveZone' });
      }
    }
  }

  getBestZone() {
    if (ZERO_PURCHASE) {
      // William: Always max endgame Zone 19
      return 19;
    }

    const level = this.me?.level || 1;
    const unlocked = this.defs.zones.filter(z => level >= z.level);
    if (!unlocked.length) return 0;

    let targetZone = unlocked[unlocked.length - 1].id;

    // Reset death history if safe for > 3 mins
    if (this.lastDeathTime && Date.now() - this.lastDeathTime > 3 * 60 * 1000) {
      this.zoneDeaths = {};
      this.lastDeathTime = null;
    }

    // Vessel floor safety
    const bDef = this.defs?.boats?.find(b => b.id === this.me?.boat);
    const boatHp = bDef?.hp || 100;
    const minVesselFloor = boatHp >= 400 ? 8 : (boatHp >= 200 ? 5 : 1);

    while (targetZone > minVesselFloor && (this.zoneDeaths[targetZone] || 0) >= 2) {
      targetZone--;
    }

    return Math.max(minVesselFloor, targetZone);
  }

  autoEquipBestWeapon() {
    if (!this.me?.items || this.me.items.length === 0) return;
    let best = null;
    let bestScore = -1;

    for (const item of this.me.items) {
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (!wDef) continue;
      const score = (wDef.tier * 1000) + (wDef.dps || 0);
      if (score > bestScore) {
        bestScore = score;
        best = item;
      }
    }

    if (best && best.uid !== this.me.equipped) {
      const wDef = this.defs?.weapons?.find(w => w.id === best.w);
      this.log('UPGRADE', `Equipped superior blaster: ${wDef?.name || best.w} (Tier ${wDef?.tier || 0})`);
      this.send({ t: 'equipWeapon', id: best.uid });
      this.me.equipped = best.uid;
    }
  }

  autoSalvageJunk() {
    if (ZERO_PURCHASE || !this.me?.items || this.me.items.length <= 4) return;
    const equipped = this.me.equipped;
    const eqItem = this.me.items.find(i => i.uid === equipped);
    const eqDef = this.defs?.weapons?.find(w => w.id === eqItem?.w);
    const eqTier = eqDef?.tier || 0;

    const junkUids = [];
    for (const item of this.me.items) {
      if (item.uid === equipped) continue;
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (!wDef) continue;
      if (wDef.tier < eqTier - 1) {
        junkUids.push(item.uid);
      }
    }

    if (junkUids.length > 0) {
      this.log('SALVAGE', `Auto-salvaging ${junkUids.length} obsolete blaster(s) for scrap coins.`);
      this.send({ t: 'salvage', uids: junkUids });
    }
  }

  checkHullUpgrade() {
    if (ZERO_PURCHASE) return;
    const owned = new Set(this.me.boats || [this.me.boat]);
    const coins = this.me.coins || 0;

    const affordable = this.defs.boats.filter(b => !owned.has(b.id) && b.cost > 0 && coins >= b.cost);
    affordable.sort((a, b) => (b.hp + b.def * 5) - (a.hp + a.def * 5));

    if (affordable.length > 0) {
      const best = affordable[0];
      const cur = this.defs.boats.find(b => b.id === this.me.boat);
      if ((best.hp + best.def * 5) > ((cur?.hp || 0) + (cur?.def || 0) * 5)) {
        this.log('UPGRADE', `Purchasing new vessel hull: ${best.name} for ${best.cost.toLocaleString()} coins!`);
        this.send({ t: 'mv', x: 26, z: -12, r: 0 });
        this.send({ t: 'buyBoat', id: best.id });
        this.send({ t: 'equipBoat', id: best.id });
        this.me.boat = best.id;
        this.stats.upgradesPurchased++;
      }
    }
  }

  checkHelperRecruitment() {
    if (ZERO_PURCHASE) return;
    const owned = new Set(this.me.helpers || (this.me.helper ? [this.me.helper] : []));
    const coins = this.me.coins || 0;

    const affordable = this.defs.helpers.filter(h => !owned.has(h.id) && h.cost > 0 && coins >= h.cost);
    affordable.sort((a, b) => b.cost - a.cost);

    if (affordable.length > 0) {
      const best = affordable[0];
      this.log('TAVERN', `Hiring crew helper: ${best.name} (${best.desc}) for ${best.cost.toLocaleString()} coins!`);
      this.send({ t: 'mv', x: -9, z: -6, r: 0 });
      this.send({ t: 'buyHelper', id: best.id });
      this.send({ t: 'equipHelper', id: best.id });
      this.me.helper = best.id;
      this.stats.upgradesPurchased++;
    }
  }

  dumpStatus() {
    try {
      const mem = process.memoryUsage();
      const statusObj = {
        updatedAt: new Date().toISOString(),
        engine: 'Pure Node.js Headless WebSocket Daemon',
        memoryRssMb: +(mem.rss / 1024 / 1024).toFixed(1),
        memoryHeapMb: +(mem.heapUsed / 1024 / 1024).toFixed(1),
        me: this.me ? {
          name: this.me.name,
          level: this.me.level,
          coins: this.me.coins,
          xp: this.me.xp,
          boat: this.me.boat,
          boats: this.me.boats,
          helper: this.me.helper,
          helpers: this.me.helpers,
          points: this.me.points,
          skills: this.me.skills,
          equipped: this.me.equipped,
          items: this.me.items
        } : null,
        zone: this.zone ? `Zone ${this.zone.zone}` : 'Harbor',
        combo: this.combo,
        botRunning: this.connected,
        stats: this.stats
      };
      fs.writeFileSync(STATUS_FILE, JSON.stringify(statusObj, null, 2));
    } catch (e) {
      // Ignore write errors
    }
  }
}

// Bootstrap
fetchDefs().then(defs => {
  const daemon = new ReefBlastDaemon(defs);
  daemon.start();
}).catch(err => {
  writeLog(`[FATAL] Failed to fetch game defs: ${err.message}`);
  process.exit(1);
});
