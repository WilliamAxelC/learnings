/**
 * Reefblast Pure Headless WebSocket Client & Auto-Progression Daemon
 * Unified Autonomous Naval Engine with Dynamic Multi-Bot Policies
 * 
 * Features:
 * - Dynamic Policy Engine: Configurable via policy.json or Discord (/reefblast policy)
 *   * allowBuying: Controls vessel hull purchase, forge weapon craft, helper hire, and market sniping.
 *   * allowSalvaging: Controls auto-salvaging junk scrap (Strictly locked by default).
 *   * allowMarketSelling: Controls listing surplus weapons on player market.
 * - Strict Purchasing Guarantee: NEVER buys or crafts any weapon, boat, or helper weaker or equal to what is already owned!
 * - Universal Zone Determination: Dynamic level-based zone unlocks (never stuck in Zone 0).
 * - Navigation: Realistic smooth sailing physics (incremental heading & velocity vectors).
 * - Auto-Adjustment: Dynamic game definitions drift detection & automatic adaptation for skills, caps, and weapons.
 */

const WebSocket = require('ws');
const fs = require('fs');
const path = require('path');
const https = require('https');

// Environment & Configuration
let TOKEN = process.env.RB_TOKEN || '';
const BOT_NAME = process.env.RB_BOT_NAME || 'cuang-bot';
const BOT_PASS = process.env.RB_BOT_PASS || 'Rb!dIkC3VsYPED15';
const LOGS_DIR = path.join(__dirname, 'logs');
const LOG_FILE = path.join(LOGS_DIR, 'auto_progression.log');
const STATUS_FILE = path.join(LOGS_DIR, 'status.json');
const POLICY_FILE = path.join(__dirname, 'policy.json');
const FINGERPRINT_FILE = path.join(LOGS_DIR, 'game_defs_fingerprint.json');
const UPDATE_DETECTED_FILE = path.join(LOGS_DIR, 'game_update_detected.json');

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

// Compute structural fingerprint for drift detection
function computeDefsFingerprint(defs) {
  if (!defs) return null;
  const activeWeapons = (defs.weapons || []).filter(w => !w.retired).map(w => w.id).sort();
  const retiredWeapons = (defs.weapons || []).filter(w => !!w.retired).map(w => w.id).sort();
  const boats = (defs.boats || []).filter(b => !b.retired).map(b => b.id).sort();
  const skills = (defs.skills || []).map(s => s.id).sort();
  const zones = (defs.zones || []).map(z => z.id).sort();

  return {
    maxLevel: defs.maxLevel || 100,
    skillMax: defs.skillMax || 30,
    skills,
    skillCount: skills.length,
    activeWeaponsCount: activeWeapons.length,
    retiredWeaponsCount: retiredWeapons.length,
    activeWeapons,
    retiredWeapons,
    boatsCount: boats.length,
    boats,
    zonesCount: zones.length,
    burn: defs.burn || null,
    leech: defs.leech || null,
    poison: defs.poison || null,
    expTier: defs.expTier || 0,
    forgeTiers: defs.forgeTiers || 0
  };
}

function compareFingerprints(oldFp, newFp) {
  if (!oldFp || !newFp) return null;
  const changes = [];

  if (newFp.maxLevel !== oldFp.maxLevel) {
    changes.push(`Max level changed from ${oldFp.maxLevel} to ${newFp.maxLevel}`);
  }
  if (newFp.skillMax !== oldFp.skillMax) {
    changes.push(`Skill max cap changed from ${oldFp.skillMax} to ${newFp.skillMax}`);
  }
  const newSkills = newFp.skills.filter(s => !oldFp.skills.includes(s));
  if (newSkills.length > 0) changes.push(`New skills added: ${newSkills.join(', ')}`);

  const newWeapons = newFp.activeWeapons.filter(w => !oldFp.activeWeapons.includes(w));
  if (newWeapons.length > 0) changes.push(`New active weapons added: ${newWeapons.join(', ')}`);

  const newlyRetired = newFp.retiredWeapons.filter(w => !oldFp.retiredWeapons.includes(w));
  if (newlyRetired.length > 0) changes.push(`Weapons retired: ${newlyRetired.join(', ')}`);

  const newBoats = newFp.boats.filter(b => !oldFp.boats.includes(b));
  if (newBoats.length > 0) changes.push(`New boats added: ${newBoats.join(', ')}`);

  if (newFp.zonesCount !== oldFp.zonesCount) {
    changes.push(`Zones count changed from ${oldFp.zonesCount} to ${newFp.zonesCount}`);
  }

  return changes.length > 0 ? changes : null;
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

    // Policy controls
    this.allowBuying = true;
    this.allowSalvaging = false;
    this.allowMarketSelling = false;
    this.readPolicy();

    // Navigation & Harbor Physics
    this.pos = { x: 0, z: 10, r: 0 };
    this.isSailing = false;
    this._harborTicks = 0;
    this._lastHarborTrip = Date.now();
    this._managerBusy = false;

    // Game state
    this.me = null;
    this.zone = null;
    this.slot = -1;
    this.combo = 0;
    this.comboT = 0;
    this.lastComboUpdate = Date.now();
    this.roster = [];
    this.fishes = new Map();
    this.shots = new Map();
    this.lastAimSent = { x: 0, z: 0, f: false, t: 0 };
    this.lastDeathTime = null;
    this.zoneDeaths = {};
    this._lastLevel = 1;
    this._lastCoins = 0;
    this._lastMarketPoll = 0;
    this._marketChecked = false;
    this._marketSniped = false;
    this._needsRespec = true;

    // Statistics
    this.stats = {
      startedAt: Date.now(),
      startCoins: 0,
      startLevel: 1,
      startXp: 0,
      bestCombo: 0,
      killsThisSession: 0,
      shotsIntercepted: 0,
      upgradesPurchased: 0,
      marketDealsSniped: 0
    };
    this._lastDefsRefresh = Date.now();
    this.defsFingerprint = computeDefsFingerprint(defs);
    this.driftDetected = null;

    // Initial check for definitions drift
    this.checkInitialDrift();
  }

  readPolicy() {
    try {
      if (fs.existsSync(POLICY_FILE)) {
        const raw = fs.readFileSync(POLICY_FILE, 'utf-8');
        const data = JSON.parse(raw);
        if (typeof data.allowBuying === 'boolean') {
          this.allowBuying = data.allowBuying;
        }
        if (typeof data.allowSalvaging === 'boolean') {
          this.allowSalvaging = data.allowSalvaging;
        }
        if (typeof data.allowMarketSelling === 'boolean') {
          this.allowMarketSelling = data.allowMarketSelling;
        }
      }
    } catch (_) {}
  }

  savePolicy() {
    try {
      fs.writeFileSync(POLICY_FILE, JSON.stringify({
        allowBuying: this.allowBuying,
        allowSalvaging: this.allowSalvaging,
        allowMarketSelling: this.allowMarketSelling,
        updatedAt: new Date().toISOString()
      }, null, 2));
    } catch (_) {}
  }

  checkInitialDrift() {
    try {
      if (fs.existsSync(FINGERPRINT_FILE)) {
        const saved = JSON.parse(fs.readFileSync(FINGERPRINT_FILE, 'utf-8'));
        const changes = compareFingerprints(saved, this.defsFingerprint);
        if (changes && changes.length > 0) {
          this.log('UPDATE_DETECTED', `🚨 Game update detected! Changes:\n  - ${changes.join('\n  - ')}`);
          this.driftDetected = { detectedAt: new Date().toISOString(), changes };
          fs.writeFileSync(UPDATE_DETECTED_FILE, JSON.stringify(this.driftDetected, null, 2));
        }
      }
      fs.writeFileSync(FINGERPRINT_FILE, JSON.stringify(this.defsFingerprint, null, 2));
    } catch (_) {}
  }

  async refreshDefs() {
    try {
      const freshDefs = await fetchDefs();
      if (freshDefs && freshDefs.weapons && freshDefs.boats) {
        const newFp = computeDefsFingerprint(freshDefs);
        const changes = compareFingerprints(this.defsFingerprint, newFp);
        if (changes && changes.length > 0) {
          this.log('UPDATE_DETECTED', `🚨 Game update detected during live refresh! (${changes.length} changes):\n  - ${changes.join('\n  - ')}`);
          this.driftDetected = { detectedAt: new Date().toISOString(), changes };
          fs.writeFileSync(UPDATE_DETECTED_FILE, JSON.stringify(this.driftDetected, null, 2));
        }
        this.defs = freshDefs;
        this.defsFingerprint = newFp;
        fs.writeFileSync(FINGERPRINT_FILE, JSON.stringify(newFp, null, 2));
        this.log('DEFS', `Game definitions refreshed: ${freshDefs.weapons.length} weapons, ${freshDefs.boats.length} boats, ${freshDefs.zones.length} zones. MaxLevel: ${freshDefs.maxLevel}, SkillMax: ${freshDefs.skillMax}`);
      }
    } catch (e) {
      this.log('DEFS', `Failed to refresh defs: ${e.message}`);
    }
  }

  log(category, msg) {
    writeLog(`[${category}] ${msg}`);
  }

  async start() {
    this.log('SYSTEM', `=== Reefblast Pure Headless WebSocket Daemon Starting (${BOT_NAME}) ===`);
    this.log('SYSTEM', `Configuration: Buying=${this.allowBuying ? 'ENABLED 🛒' : 'DISABLED 🛡️'} | Salvaging=${this.allowSalvaging ? 'ENABLED ♻️' : 'LOCKED 🔒'} | MarketSelling=${this.allowMarketSelling ? 'ENABLED 🏪' : 'DISABLED 🔒'}`);

    if (!TOKEN) {
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
      this.isSailing = false;
      const reasonStr = reason ? reason.toString() : '';
      this.log('SYSTEM', `WebSocket closed (code: ${code}, reason: "${reasonStr}").`);

      if (reasonStr.includes('session expired') || code === 4001 || code === 1008) {
        this.log('SYSTEM', 'Token expired. Refreshing login via API...');
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
        this.pos = { x: 0, z: 10, r: 0 };
        this._lastLevel = m.me?.level || 1;
        this._lastCoins = m.me?.coins || 0;
        if (this.stats.startCoins === 0) {
          this.stats.startCoins = m.me?.coins || 0;
          this.stats.startLevel = m.me?.level || 1;
          this.stats.startXp = m.me?.xp || 0;
        }
        this.log('SYSTEM', `Captain ${m.me.name} ready! Level: ${m.me.level} | Coins: ${m.me.coins.toLocaleString()} | Vessel: ${m.me.boat}`);
        this.autoEquipBestWeapon();
        this.autoEquipBestBoat();
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

      case 'pos':
        this.pos.x = m.x;
        this.pos.z = m.z;
        break;

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
        this.isSailing = false;
        if (m.party || this.inParty) {
          this.inParty = true;
          this.lastPartyActivity = Date.now();
        }
        this.partyWaitingInHarbor = false;
        const zoneName = this.defs?.zones[m.zone]?.name || `Zone ${m.zone}`;
        this.log('ZONE_CHANGE', `Entered ${zoneName} (Slot ${m.slot}). Combat active!`);
        break;
      }

      case 'left':
        this.zone = null;
        this.slot = -1;
        this.pos = { x: 0, z: 10, r: 0 };
        this.fishes.clear();
        this.shots.clear();
        this.isSailing = false;
        this._harborTicks = 0;
        if (m.me) this.me = m.me;
        if (this.inParty) {
          this.partyWaitingInHarbor = true;
          this.lastPartyActivity = Date.now();
          this.log('PARTY', `Returned to Harbor with party. Waiting for leader to vote/regroup...`);
        } else {
          this.log('ZONE_CHANGE', `Left zone, returned to Harbor.`);
        }
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
          this.log('PARTY', `Incoming party invite from ${m.from}. Auto-accepting as carry!`);
          this.send({ t: 'partyAccept', name: m.from });
          this.inParty = true;
          this.partyLeader = m.from;
          this.partyWaitingInHarbor = true;
          this.lastPartyActivity = Date.now();
          if (this.zone) {
            this.log('PARTY', `Leaving solo zone to Harbor to regroup with party leader ${m.from}...`);
            this.send({ t: 'leaveZone' });
          }
        }
        break;

      case 'vote':
        this.inParty = true;
        this.lastPartyActivity = Date.now();
        this.log('PARTY', 'Party area vote proposed. Auto-voting YES to carry!');
        this.send({ t: 'vote', yes: true });
        break;

      case 'party':
        this.inParty = true;
        this.lastPartyActivity = Date.now();
        break;

      case 'partyLeave':
      case 'partyDisband':
      case 'partyKick':
        this.inParty = false;
        this.partyWaitingInHarbor = false;
        this.partyLeader = null;
        this.log('PARTY', `Party ended (${m.t}). Resuming solo progression.`);
        break;

      case 'market':
        this.handleMarketList(m);
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
      valid.sort((a, b) => {
        const distA = Math.hypot(a.x - origin.x, a.z - origin.z);
        const distB = Math.hypot(b.x - origin.x, b.z - origin.z);
        return (a.hp * 2 + distA) - (b.hp * 2 + distB);
      });
      target = valid[0];
    } else {
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

  // Realistic Sailing Physics Navigator
  async sailTo(targetX, targetZ, stopRadius = 3.5, maxDurationMs = 7000) {
    if (!this.pos) this.pos = { x: 0, z: 10, r: 0 };
    this.isSailing = true;
    const startTime = Date.now();
    const curBoat = this.defs?.boats?.find(b => b.id === this.me?.boat);
    const baseSpeed = curBoat?.speed || 12;
    // Responsive cruising speed (15 - 24 units/sec)
    const speed = Math.max(14, Math.min(24, baseSpeed * 1.35));
    const dt = 0.1; // 100ms per tick

    while (Date.now() - startTime < maxDurationMs) {
      const dx = targetX - this.pos.x;
      const dz = targetZ - this.pos.z;
      const dist = Math.hypot(dx, dz);

      if (dist <= stopRadius) {
        break;
      }

      const wantR = Math.atan2(dx, dz);
      // Smooth angular turn towards heading
      const diffR = Math.atan2(Math.sin(wantR - this.pos.r), Math.cos(wantR - this.pos.r));
      this.pos.r += diffR * Math.min(1, dt * 5.5);

      // Increment position forward
      const moveStep = Math.min(speed * dt, dist);
      this.pos.x += Math.sin(wantR) * moveStep;
      this.pos.z += Math.cos(wantR) * moveStep;

      this.send({
        t: 'mv',
        x: +this.pos.x.toFixed(2),
        z: +this.pos.z.toFixed(2),
        r: +this.pos.r.toFixed(2)
      });

      await new Promise(res => setTimeout(res, 100));
    }

    this.isSailing = false;
  }

  // Progression loop (every 3 seconds)
  async managerStep() {
    if (!this.connected || !this.me || this._managerBusy) return;
    this._managerBusy = true;

    try {
      this.readPolicy();

      // 0. Respec to align existing points with AM-GM optimal curve
      if (this._needsRespec) {
        if (this.zone) {
          this.log('SKILL', 'Returning to Harbor briefly to respec skills into optimal AM-GM curve...');
          this.send({ t: 'leaveZone' });
          return;
        } else {
          this.log('SKILL', 'Resetting skill tree to redistribute all points into AM-GM optimal curve...');
          this.send({ t: 'respec' });
          this._needsRespec = false;
          await new Promise(r => setTimeout(r, 600));
          return;
        }
      }

      // 1. Dynamic Auto-allocate Skill Points on Level Up (AM-GM Optimal Ratio)
      if (this.me.points > 0) {
        const maxPerSkill = this.defs?.skillMax || 50;
        const availableSkills = (this.defs?.skills || []).map(s => s.id);
        const ratios = { atk: 0.30, aspd: 0.25, crit: 0.18, def: 0.15, hp: 0.08, regen: 0.04 };
        const regenCap = 5;

        for (const sk of availableSkills) {
          if (!ratios[sk]) ratios[sk] = 0.05;
        }

        while (this.me.points > 0) {
          let bestStat = null;
          let lowestScore = Infinity;

          for (const [stat, weight] of Object.entries(ratios)) {
            if (!availableSkills.includes(stat)) continue;
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

          if (!bestStat) {
            for (const s of availableSkills) {
              const current = (this.me.skills && this.me.skills[s]) || 0;
              if (current < maxPerSkill) {
                bestStat = s;
                break;
              }
            }
          }

          if (!bestStat) break;

          const curVal = (this.me.skills && this.me.skills[bestStat]) || 0;
          const cap = bestStat === 'regen' && curVal < regenCap ? regenCap : maxPerSkill;
          const pointsToSpend = Math.min(this.me.points, cap - curVal, 1);
          if (pointsToSpend <= 0) break;

          this.send({ t: 'skill', id: bestStat, n: pointsToSpend });
          if (!this.me.skills) this.me.skills = {};
          this.me.skills[bestStat] = curVal + pointsToSpend;
          this.me.points -= pointsToSpend;
          this.log('SKILL', `Allocated skill point to: ${bestStat.toUpperCase()} (${curVal + pointsToSpend}/${maxPerSkill})`);
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

      // Periodic definitions refresh (every 20 mins)
      if (Date.now() - (this._lastDefsRefresh || 0) > 20 * 60 * 1000) {
        this._lastDefsRefresh = Date.now();
        await this.refreshDefs();
      }

      // 3. Auto-Equip Best Weapon in Bag
      this.autoEquipBestWeapon();

      // 4. Auto-Equip Best Boat in Fleet
      this.autoEquipBestBoat();

      // 5. Zone or Harbor Actions
      if (!this.zone) {
        // Harbor
        if (this._harborTicks === undefined) this._harborTicks = 0;
        this._harborTicks++;

        if (this.allowBuying) {
          // STEP 1: Check market listings
          if (this._harborTicks === 1) {
            this._marketChecked = false;
            this._marketSniped = false;
            this._lastMarketPoll = Date.now();
            this.log('HARBOR', 'Sailing to Market dock (9, -4) to inspect deals...');
            await this.sailTo(9, -4, 3.5);
            this.send({ t: 'mkList' });
            return;
          }

          // STEP 2: Harbor upgrades & crafting
          if (this._harborTicks === 2) {
            if (!this._marketSniped) {
              await this.checkForgeCrafting();
            }
            await this.checkHullUpgrade();
            await this.checkHelperRecruitment();
            if (this.allowMarketSelling) {
              await this.checkMarketSelling();
            }
            if (this.allowSalvaging) {
              this.autoSalvageJunk();
            }
            return;
          }
        } else {
          if (this.allowMarketSelling) {
            await this.checkMarketSelling();
          }
          if (this.allowSalvaging) {
            this.autoSalvageJunk();
          }
        }

        // STEP 3: Sail smoothly to target gate and enter (Solo only)
        if (this.inParty) {
          if (Date.now() - (this.lastPartyActivity || 0) > 480000) {
            this.inParty = false;
            this.partyWaitingInHarbor = false;
            this.partyLeader = null;
            this.log('PARTY', 'Party idle timeout in Harbor (8m). Resuming solo progression.');
          } else {
            if (this._harborTicks % 10 === 1) {
              this.log('PARTY', `Holding position in Harbor in Carry Mode. Waiting for leader '${this.partyLeader || ''}' to initiate area vote...`);
            }
            return;
          }
        }

        const targetZone = this.getBestZone();
        const gate = this.defs?.harbor?.gates?.find(g => g.zone === targetZone);
        const zoneDef = this.defs?.zones?.[targetZone];
        if (gate) {
          this.log('ZONE_CHANGE', `Sailing towards Zone ${targetZone} Gate at (${gate.x}, ${gate.z})...`);
          await this.sailTo(gate.x, gate.z, 5.0);
        }
        this.log('ZONE_CHANGE', `Setting sail into Zone ${targetZone}: ${zoneDef?.name || 'Open Waters'} (Lv ${zoneDef?.level || 1}+)`);
        this.send({ t: 'enter', zone: targetZone });
      } else {
        // At Sea
        if (this.inParty) {
          // Dedicated carry mode: NEVER voluntarily leave the zone while in a party!
          return;
        }
        const timeAtSea = (Date.now() - this._lastHarborTrip) / 1000;
        const curZone = this.zone.zone;
        const bestZone = this.getBestZone();

        const outgrownZone = curZone < bestZone;
        const hasSurplusCoins = (this.me?.coins || 0) > 1500000;
        const routineShopTrip = this.allowBuying && hasSurplusCoins && timeAtSea > 240;
        const maxVoyageLimit = timeAtSea > 420;

        if ((outgrownZone || routineShopTrip || maxVoyageLimit) && timeAtSea > 15) {
          this.log('UPGRADE', `Harbor trip initiated (Zone: ${curZone} vs Best: ${bestZone}, Coins: ${this.me?.coins?.toLocaleString()}, Time at sea: ${Math.round(timeAtSea)}s). Returning to Harbor...`);
          this._lastHarborTrip = Date.now();
          this.send({ t: 'leaveZone' });
        }
      }
    } finally {
      this._managerBusy = false;
    }
  }

  // Universal dynamic zone selector based on level and survival
  getBestZone() {
    const playerLevel = this.me?.level || 1;
    let highest = 0;
    const zones = this.defs?.zones || [];
    for (let z = 0; z < zones.length; z++) {
      const zDef = zones[z];
      if (!zDef) continue;
      const zReqLevel = zDef.level || 0;
      const deathsInZone = this.zoneDeaths[z] || 0;
      if (deathsInZone >= 3 && Date.now() - (this.lastDeathTime || 0) < 180000) {
        continue;
      }
      if (playerLevel >= zReqLevel) {
        highest = z;
      }
    }
    return highest;
  }

  // Calculate comprehensive weapon score accounting for Tier 6/7, multi-traits, and power level
  calcWeaponScore(wDef, item) {
    if (!wDef) return -1;
    const isExclusive = !!wDef.exclusive;
    const playerLevel = this.me?.level || 100;
    const effTier = isExclusive ? 6.5 : (wDef.tier || 0);

    let baseDps = wDef.dps || 0;
    if (wDef.scales && this.defs?.tierDps && this.defs?.exclusiveEdge) {
      const t = Math.min(6, Math.floor(playerLevel / 16) + 1);
      baseDps = (this.defs.tierDps[t] || 10945) * (this.defs.exclusiveEdge || 1.08);
    }
    if (!baseDps && wDef.damage && wDef.fireRate) {
      baseDps = wDef.damage * (1000 / wDef.fireRate);
    }

    const rollAtk = 1 + (item?.atk || 0);
    const rollAspd = 1 + (item?.aspd || 0);
    const rollCrit = 1 + (item?.crit || 0);
    const effectiveDps = baseDps * rollAtk * rollAspd * rollCrit;

    const traits = new Set(wDef.traits || (wDef.kind ? [wDef.kind] : []));
    let traitMult = 1.0;
    if (traits.has('homing')) traitMult += 0.15;
    if (traits.has('explosive')) traitMult += 0.12;
    if (traits.has('chain')) traitMult += 0.10;
    if (traits.has('lifesteal')) traitMult += 0.14;
    if (traits.has('poison')) traitMult += 0.12;
    if (traits.has('burn')) traitMult += 0.10;
    if (traits.has('pierce')) traitMult += 0.08;
    if (traits.has('spread')) traitMult += 0.06;

    const plBonus = (wDef.pl || 0) * 0.1;
    return (effTier * 2000) + (effectiveDps * traitMult) + plBonus;
  }

  // Calculate comprehensive boat score accounting for Tier 7 Experimental, Tier 6, EHP, and combat buffs
  calcBoatScore(bDef) {
    if (!bDef) return -1;
    const isExclusive = !!bDef.exclusive;
    const effTier = isExclusive ? 6.5 : (bDef.tier || 0);

    const ehp = (bDef.hp || 100) * (1 + ((bDef.def || 0) / 100));
    const dmgBuff = (bDef.buffs?.atk || 0) * 1000000;
    const aspdBuff = (bDef.buffs?.aspd || 0) * 1000000;
    const critBuff = (bDef.buffs?.crit || 0) * 800000;
    const regenScore = (bDef.buffs?.regen || 0) * 2000000;
    const econScore = ((bDef.buffs?.coin || 0) + (bDef.buffs?.xp || 0)) * 10000;
    const plScore = (bDef.pl || 0) * 2;

    return (effTier * 20000) + (ehp * 0.5) + dmgBuff + aspdBuff + critBuff + regenScore + econScore + plScore;
  }

  calcHelperScore(hDef) {
    if (!hDef) return -1;
    return ((hDef.dps || 0) * 10) + (hDef.pl || 0);
  }

  // Switch guns to best owned weapon
  autoEquipBestWeapon() {
    if (!this.me?.items || this.me.items.length === 0) return;
    let best = null;
    let bestScore = -1;

    for (const item of this.me.items) {
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (!wDef) continue;
      const score = this.calcWeaponScore(wDef, item);
      if (score > bestScore) {
        bestScore = score;
        best = item;
      }
    }

    if (best && best.uid !== this.me.equipped) {
      const wDef = this.defs?.weapons?.find(w => w.id === best.w);
      this.log('GEAR', `Switched weapon to: ${wDef?.name || best.w} (Tier ${wDef?.tier || 0}, DPS: ${wDef?.dps || 0}, PL: ${wDef?.pl || 0}, UID: ${best.uid})`);
      this.send({ t: 'equipWeapon', id: best.uid });
      this.me.equipped = best.uid;
    }
  }

  // Switch ships to best owned boat
  autoEquipBestBoat() {
    if (!this.me?.boats || this.me.boats.length <= 1) return;
    let bestBoat = this.me.boat;
    let bestScore = -1;

    for (const bId of this.me.boats) {
      const bDef = this.defs?.boats?.find(b => b.id === bId);
      if (!bDef) continue;
      const score = this.calcBoatScore(bDef);
      if (score > bestScore) {
        bestScore = score;
        bestBoat = bId;
      }
    }

    if (bestBoat && bestBoat !== this.me.boat) {
      const bDef = this.defs?.boats?.find(b => b.id === bestBoat);
      this.log('GEAR', `Switched vessel to: ${bDef?.name || bestBoat} (Tier ${bDef?.tier || 0}, HP: ${bDef?.hp}, DEF: ${bDef?.def}, PL: ${bDef?.pl})`);
      this.send({ t: 'equipBoat', id: bestBoat });
      this.me.boat = bestBoat;
    }
  }

  // STRICT GUARANTEE: Never buy a vessel hull weaker or equal to what we already own
  async checkHullUpgrade() {
    if (!this.allowBuying) return;
    const owned = new Set(this.me?.boats || (this.me?.boat ? [this.me?.boat] : []));
    const coins = this.me?.coins || 0;

    let bestOwnedScore = 0;
    let bestOwnedTier = 0;
    for (const bId of owned) {
      const bDef = this.defs?.boats?.find(b => b.id === bId);
      if (bDef) {
        bestOwnedScore = Math.max(bestOwnedScore, this.calcBoatScore(bDef));
        bestOwnedTier = Math.max(bestOwnedTier, bDef.tier || 0);
      }
    }

    const affordable = this.defs.boats.filter(b => !b.retired && !owned.has(b.id) && b.cost > 0 && coins >= b.cost);
    affordable.sort((a, b) => this.calcBoatScore(b) - this.calcBoatScore(a));

    if (affordable.length > 0) {
      const candidate = affordable[0];
      const candScore = this.calcBoatScore(candidate);
      if (candScore > bestOwnedScore * 1.02 && (candidate.tier || 0) >= bestOwnedTier) {
        this.log('UPGRADE', `Purchasing superior vessel hull: ${candidate.name} (Tier ${candidate.tier || 0}, Score ${Math.round(candScore)} vs Best Owned: ${Math.round(bestOwnedScore)}) for ${candidate.cost.toLocaleString()} coins!`);
        await this.sailTo(26, -9, 3.5);
        this.send({ t: 'buyBoat', id: candidate.id });
        this.send({ t: 'equipBoat', id: candidate.id });
        this.me.boat = candidate.id;
        if (!this.me.boats) this.me.boats = [];
        this.me.boats.push(candidate.id);
        this.stats.upgradesPurchased++;
      }
    }
  }

  // STRICT GUARANTEE: Never hire a helper weaker or equal to what we already own
  async checkHelperRecruitment() {
    if (!this.allowBuying) return;
    const owned = new Set(this.me?.helpers || (this.me?.helper ? [this.me?.helper] : []));
    const coins = this.me?.coins || 0;

    let bestOwnedScore = 0;
    for (const hId of owned) {
      const hDef = this.defs?.helpers?.find(h => h.id === hId);
      if (hDef) {
        bestOwnedScore = Math.max(bestOwnedScore, this.calcHelperScore(hDef));
      }
    }

    const affordable = this.defs.helpers.filter(h => !owned.has(h.id) && h.cost > 0 && coins >= h.cost);
    affordable.sort((a, b) => this.calcHelperScore(b) - this.calcHelperScore(a));

    if (affordable.length > 0) {
      const candidate = affordable[0];
      const candScore = this.calcHelperScore(candidate);
      if (candScore > bestOwnedScore) {
        this.log('TAVERN', `Hiring superior crew helper: ${candidate.name} (${candidate.desc}, Score: ${candScore} vs Best Owned: ${bestOwnedScore}) for ${candidate.cost.toLocaleString()} coins!`);
        await this.sailTo(-9, -4, 3.5);
        this.send({ t: 'buyHelper', id: candidate.id });
        this.send({ t: 'equipHelper', id: candidate.id });
        this.me.helper = candidate.id;
        if (!this.me.helpers) this.me.helpers = [];
        this.me.helpers.push(candidate.id);
        this.stats.upgradesPurchased++;
      }
    }
  }

  // STRICT GUARANTEE: Never craft a weapon weaker or equal to what we already own
  async checkForgeCrafting() {
    if (!this.allowBuying) return;
    const coins = this.me?.coins || 0;

    let bestOwnedScore = 0;
    let bestOwnedTier = 0;
    for (const item of (this.me?.items || [])) {
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (wDef) {
        bestOwnedScore = Math.max(bestOwnedScore, this.calcWeaponScore(wDef, item));
        bestOwnedTier = Math.max(bestOwnedTier, wDef.tier || 0);
      }
    }

    const craftable = this.defs.weapons.filter(w => !w.retired && !w.exclusive && w.cost > 0 && coins >= w.cost);
    craftable.sort((a, b) => this.calcWeaponScore(b, null) - this.calcWeaponScore(a, null));

    if (craftable.length > 0) {
      const candidate = craftable[0];
      const candScore = this.calcWeaponScore(candidate, null);
      if (candScore > bestOwnedScore * 1.02 && (candidate.tier || 0) >= bestOwnedTier) {
        this.log('FORGE', `Crafting superior weapon at Forge: ${candidate.name} (Tier ${candidate.tier}, Score: ${Math.round(candScore)} vs Best Owned: ${Math.round(bestOwnedScore)}) for ${candidate.cost.toLocaleString()} coins!`);
        await this.sailTo(-26, -9, 3.5);
        this.send({ t: 'craft', id: candidate.id, n: 1 });
        this.stats.upgradesPurchased++;
      }
    }
  }

  // STRICT GUARANTEE: Never snipe a market deal weaker or equal to what we already own
  async handleMarketList(m) {
    this._marketChecked = true;
    if (!this.allowBuying) return;
    if (!m?.listings || !m.listings.length) {
      this.log('MARKET', 'Market scanned: No active listings available right now.');
      return;
    }
    const coins = this.me?.coins || 0;

    let bestOwnedScore = 0;
    let bestOwnedTier = 0;
    for (const item of (this.me?.items || [])) {
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (wDef) {
        bestOwnedScore = Math.max(bestOwnedScore, this.calcWeaponScore(wDef, item));
        bestOwnedTier = Math.max(bestOwnedTier, wDef.tier || 0);
      }
    }

    let bestDeal = null;
    let bestDealScore = -1;

    for (const listing of m.listings) {
      if (listing.seller === this.me?.name) continue;
      if (coins < listing.price) continue;
      const wDef = this.defs?.weapons?.find(w => w.id === listing.item.w);
      if (!wDef) continue;

      const score = this.calcWeaponScore(wDef, listing.item);
      if (score > bestOwnedScore * 1.02 && (wDef.tier || 0) >= bestOwnedTier && score > bestDealScore) {
        bestDealScore = score;
        bestDeal = { listing, wDef, score };
      }
    }

    if (bestDeal) {
      const { listing, wDef, score } = bestDeal;
      this.log('MARKET', `🎯 SNIPING MARKET DEAL: Found superior ${wDef.name} (Tier ${wDef.tier}, Score: ${Math.round(score)} vs Best Owned: ${Math.round(bestOwnedScore)}) for ${listing.price.toLocaleString()} coins!`);
      await this.sailTo(9, -4, 3.5);
      this.send({ t: 'mkBuy', id: listing.id });
      this._marketSniped = true;
      this.stats.marketDealsSniped++;
      this.stats.upgradesPurchased++;
    } else {
      this.log('MARKET', `Market scanned: ${m.listings.length} listings checked, no superior weapon upgrades found.`);
    }
  }

  // Market Selling module for surplus loot
  async checkMarketSelling() {
    if (!this.allowMarketSelling) return;
    if (!this.me?.items || this.me.items.length <= 2) return;

    let bestUid = this.me.equipped;
    let bestScore = -1;
    for (const item of this.me.items) {
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (!wDef) continue;
      const score = this.calcWeaponScore(wDef, item);
      if (score > bestScore) {
        bestScore = score;
        bestUid = item.uid;
      }
    }

    for (const item of this.me.items) {
      if (item.uid === this.me.equipped || item.uid === bestUid) continue;
      if (item.bound || item.lock) continue;
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (!wDef) continue;

      const baseCost = wDef.cost || (wDef.tier * 20000) || 5000;
      const qualityMultiplier = 1 + (item.atk || 0) + (item.aspd || 0) + (item.crit || 0);
      const askingPrice = Math.max(1000, Math.round(baseCost * 0.85 * qualityMultiplier));

      this.log('MARKET', `Listing surplus blaster for sale: ${wDef.name} (Tier ${wDef.tier}, UID: ${item.uid}) at ${askingPrice.toLocaleString()} coins...`);
      await this.sailTo(9, -4, 3.5);
      this.send({ t: 'mkSell', id: item.uid, price: askingPrice });
      break;
    }
  }

  // Auto-salvaging junk scrap (Strictly locked unless allowSalvaging is true)
  autoSalvageJunk() {
    if (!this.allowSalvaging) return;
    if (!this.me?.items || this.me.items.length <= 4) return;

    let bestScore = -1;
    for (const item of this.me.items) {
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (wDef) {
        bestScore = Math.max(bestScore, this.calcWeaponScore(wDef, item));
      }
    }

    const junkUids = [];
    for (const item of this.me.items) {
      if (item.uid === this.me.equipped || item.bound || item.lock) continue;
      const wDef = this.defs?.weapons?.find(w => w.id === item.w);
      if (!wDef) continue;
      if (wDef.tier >= 6 || wDef.exclusive) continue;
      const itemScore = this.calcWeaponScore(wDef, item);
      if (itemScore < bestScore * 0.65) {
        junkUids.push(item.uid);
      }
    }

    if (junkUids.length > 0) {
      this.log('SALVAGE', `Auto-salvaging ${junkUids.length} obsolete blaster(s) for scrap coins.`);
      this.send({ t: 'salvage', uids: junkUids });
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
        policy: {
          allowBuying: this.allowBuying,
          allowSalvaging: this.allowSalvaging,
          allowMarketSelling: this.allowMarketSelling
        },
        driftDetected: this.driftDetected,
        pos: this.pos,
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
    } catch (e) {}
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
