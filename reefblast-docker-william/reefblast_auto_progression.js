// ==UserScript==
// @name         Reefblast Auto-Progression & Deal Hunter (Pro Edition)
// @namespace    https://greentea.id/reefblast/
// @version      3.0.0
// @description  Automated combat, combo preserver, smart market sniper, forge crafter, tavern helper recruiter, inventory auto-salvage, and live event logger.
// @author       Antigravity Automation Engineering
// @match        https://greentea.id/reefblast/*
// @grant        none
// ==/UserScript==

(function () {
  'use strict';

  // Safely cleanup previous instances to prevent duplicate execution
  if (window.__ReefBlastBot) {
    try {
      if (typeof window.__ReefBlastBot.destroy === 'function') window.__ReefBlastBot.destroy();
    } catch (e) {}
    delete window.__ReefBlastBot;
  }

  const AutoBot = {
    running: false,
    combatTimer: null,
    managerTimer: null,
    marketTimer: null,
    audioKeepAlive: null,
    _inManager: false,
    _claimPending: false,
    _lastMarketPoll: 0,
    _buyingMarketItem: false,
    _boughtListings: {},
    _failedMarketListings: {},
    _zoneDeaths: {},
    _lastDeathTime: null,
    _savingGoal: 0,
    _boundWsHandler: null,
    _hookedWs: null,
    _hookedBoom: false,
    _lastLevel: 1,
    _lastCoins: 0,
    _lastActiveZone: 0,
    _lastProcessedChatAt: 0,
    
    partyState: {
      inParty: false,
      waitingInHarbor: false
    },

    geminiConfig: {
      enabled: true,
      apiKey: process.env.GEMINI_API_KEY || "",
      model: "gemini-3.5-flash-lite",
      cooldownMs: 8000,
      lastReplyTime: 0
    },

    config: {
      skillPriority: ['atk', 'def', 'hp', 'aspd', 'regen', 'crit'],
      leadTargeting: true,
      defendIncomingShots: true,
      comboSaverThreshold: 0.85, // seconds left on combo timer to trigger emergency kill
      autoZoneAdvance: true,
      autoSkills: true,
      autoQuests: true,
      autoBuyBoats: true,
      autoHireHelpers: true,
      autoCraftWeapons: true,
      autoSnipeMarketDeals: true,
      autoSalvageJunk: true,
      autoDailyChallenges: true,
      autoPartyAccept: true,
      autoPartyVote: true,
      geminiChat: true,
      minMarketDiscountPct: 25, // Buy if >= 25% cheaper than baseline/forge cost
      combatTickMs: 40,
      managerTickMs: 1500,
      marketPollIntervalMs: 12000
    },

    stats: {
      startedAt: Date.now(),
      startCoins: 0,
      startLevel: 1,
      startXp: 0,
      killsThisSession: 0,
      bestCombo: 0,
      bossesDefeated: 0,
      upgradesPurchased: 0,
      shotsIntercepted: 0,
      marketDealsSniped: 0,
      junkSalvaged: 0,
      salvageCoinsEarned: 0
    },

    bestMarketDeal: null,
    savingGoal: 0,
    logs: [],
    maxLogEntries: 500,
    ui: null,

    // --------------------------------------------------
    // LOGGING SYSTEM
    // --------------------------------------------------
    log(type, message, details = {}) {
      const now = new Date();
      const timestamp = now.toISOString();
      const timeStr = now.toTimeString().split(' ')[0];

      const entry = {
        timestamp,
        timeStr,
        type,
        message,
        details: {
          level: window.__S?.me?.level || 0,
          coins: window.__S?.me?.coins || 0,
          zone: window.__S?.zone?.zone ?? 'Harbor',
          ...details
        }
      };

      this.logs.unshift(entry);
      if (this.logs.length > this.maxLogEntries) this.logs.pop();

      try {
        localStorage.setItem('rb_progression_logs', JSON.stringify(this.logs.slice(0, 100)));
      } catch (e) {}

      console.log(`[${timeStr}] [${type}] ${message}`);
      this.updateLogUI(entry);
    },

    exportLogs() {
      const jsonStr = JSON.stringify(this.logs, null, 2);
      const blob = new Blob([jsonStr], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `reefblast_log_${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
      a.click();
      URL.revokeObjectURL(url);
    },

    getRecentLogs(n = 20) {
      return this.logs.slice(0, n);
    },

    // --------------------------------------------------
    // INITIALIZATION & LIFECYCLE
    // --------------------------------------------------
    init() {
      try {
        const saved = localStorage.getItem('rb_progression_logs');
        if (saved) this.logs = JSON.parse(saved);
        const failed = localStorage.getItem('rb_failed_listings');
        if (failed) this._failedMarketListings = JSON.parse(failed);
      } catch (e) {}

      if (document.body) {
        this.createUI();
      } else {
        window.addEventListener('DOMContentLoaded', () => this.createUI());
      }

      this.bindHotkey();
      this.hookGameEvents();
      window.__ReefBlastBot = this;
      this.log('SYSTEM', 'Auto-Progression & Deal Hunter (Pro Edition) initialized.');
    },

    bindHotkey() {
      if (this._keyHandler) window.removeEventListener('keydown', this._keyHandler);
      this._keyHandler = (e) => {
        if (e.code === 'F2') {
          e.preventDefault();
          this.toggle();
        }
      };
      window.addEventListener('keydown', this._keyHandler);
    },

    toggle() {
      if (this.running) this.stop(); else this.start();
    },

    start() {
      if (this.running) return;
      const S = window.__S;
      if (!S || !S.me) return;

      this.running = true;
      this.stats.startedAt = Date.now();
      this.stats.startCoins = S.me?.coins || 0;
      this.stats.startLevel = S.me?.level || 1;
      this.stats.startXp = S.me?.xp || 0;
      this.stats.bestCombo = S.combo || 0;

      if (this.combatTimer) clearInterval(this.combatTimer);
      if (this.managerTimer) clearInterval(this.managerTimer);
      if (this.marketTimer) clearInterval(this.marketTimer);

      this.combatTimer = setInterval(() => this.combatStep(), this.config.combatTickMs);
      this.managerTimer = setInterval(() => this.managerStep(), this.config.managerTickMs);
      this.marketTimer = setInterval(() => this.pollMarket(), this.config.marketPollIntervalMs);

      this.startKeepAlive();
      this.updateUI();
      this.pollMarket(); // Initial market fetch
      this.log('SYSTEM', `Bot started for Captain ${S.me.name} (Lv ${S.me.level}).`);
    },

    stop() {
      this.running = false;
      if (this.combatTimer) { clearInterval(this.combatTimer); this.combatTimer = null; }
      if (this.managerTimer) { clearInterval(this.managerTimer); this.managerTimer = null; }
      if (this.marketTimer) { clearInterval(this.marketTimer); this.marketTimer = null; }
      this.stopKeepAlive();
      if (window.__S) window.__S.autofire = false;
      this.updateUI();
      this.log('SYSTEM', 'Bot paused by user.');
    },

    destroy() {
      this.stop();
      if (this._keyHandler) window.removeEventListener('keydown', this._keyHandler);
      if (this._hookedWs && this._boundWsHandler) {
        try { this._hookedWs.removeEventListener('message', this._boundWsHandler); } catch (e) {}
      }
      this._hookedWs = null;
      this._boundWsHandler = null;
      if (this.ui) { this.ui.remove(); this.ui = null; }
      delete window.__ReefBlastBot;
    },

    startKeepAlive() {
      try {
        if (!this.audioKeepAlive) {
          const AudioCtx = window.AudioContext || window.webkitAudioContext;
          if (AudioCtx) {
            const ctx = new AudioCtx(), osc = ctx.createOscillator(), gain = ctx.createGain();
            gain.gain.value = 0.00001; osc.connect(gain); gain.connect(ctx.destination); osc.start();
            this.audioKeepAlive = { ctx, osc };
          }
        }
      } catch (err) {}
    },

    stopKeepAlive() {
      if (this.audioKeepAlive) {
        try { this.audioKeepAlive.osc.stop(); this.audioKeepAlive.ctx.close(); } catch (e) {}
        this.audioKeepAlive = null;
      }
    },

    hookGameEvents() {
      const world = window.__world;
      if (world && !this._hookedBoom) {
        this._hookedBoom = true;
        const origBoom = world.onBoom;
        world.onBoom = () => {
          if (origBoom) origBoom.apply(world, arguments);
          this.stats.killsThisSession++;
        };
      }

      // Single, idempotent WebSocket message hook
      const S = window.__S;
      if (S?.ws && S.ws !== this._hookedWs) {
        if (this._hookedWs && this._boundWsHandler) {
          try { this._hookedWs.removeEventListener('message', this._boundWsHandler); } catch (e) {}
        }
        this._hookedWs = S.ws;

        this._boundWsHandler = (event) => {
          try {
            const m = typeof event.data === 'string' ? JSON.parse(event.data) : event.data;
            if (!m || !m.t) return;

            if (m.t === 'chat') {
              const chatObj = m.m || m;
              if (chatObj && chatObj.at && chatObj.at === this._lastProcessedChatAt) return;
              if (chatObj && chatObj.at) this._lastProcessedChatAt = chatObj.at;
              this.handleIncomingChat(chatObj);
            } else if (m.t === 'vote') {
              if (this.config.autoPartyVote) {
                this.log('PARTY', 'Area vote proposed by party leader. Auto-voting YES!');
                window.__send({ t: 'vote', yes: true });
              }
            } else if (m.t === 'invite') {
              if (this.config.autoPartyAccept) {
                this.log('PARTY', `Incoming party invite from Captain ${m.from}. Accepting!`);
                window.__send({ t: 'partyAccept', name: m.from });
                if (S.zone) {
                  this.log('PARTY', 'Leaving zone to Harbor to regroup with party leader.');
                  window.__send({ t: 'leaveZone' });
                }
                this.partyState.inParty = true;
                this.partyState.waitingInHarbor = true;
              }
            } else if (m.t === 'left') {
              if (m.reason === 'sunk') {
                const z = this._lastActiveZone ?? S?.zone?.zone ?? 7;
                this._zoneDeaths = this._zoneDeaths || {};
                this._zoneDeaths[z] = (this._zoneDeaths[z] || 0) + 1;
                this._lastDeathTime = Date.now();
                this.log('DEFENSE', `Vessel sank in Zone ${z}! Recorded sinks in this zone: ${this._zoneDeaths[z]}. Adjusting zone tier for survivability.`);
              }
            } else if (m.t === 'joined') {
              if (typeof m.zone === 'number') this._lastActiveZone = m.zone;
            } else if (m.t === 'toast') {
              if (m.msg && m.msg.includes('Bought')) {
                this.log('UPGRADE', `Market transaction confirmed: ${m.msg}`);
              }
            }
          } catch (err) {}
        };

        S.ws.addEventListener('message', this._boundWsHandler);
      }

      this._lastLevel = window.__S?.me?.level || 1;
      this._lastCoins = window.__S?.me?.coins || 0;
    },

    // --------------------------------------------------
    // SMART PARTY & AREA INVITATION MANAGEMENT
    // --------------------------------------------------
    handlePartyManagement() {
      const S = window.__S;
      if (!S) return;

      const currentParty = S.party;
      const isCurrentlyInParty = !!(currentParty && currentParty.members && currentParty.members.length > 1);

      // Check active popups
      if (S.popups && S.popups.length > 0) {
        for (const popup of S.popups) {
          if (popup.kind === 'invite' && !popup.answered) {
            popup.answered = true;
            this.log('PARTY', `Popup: Party invitation from Captain ${popup.from}. Accepting!`);
            window.__send({ t: 'partyAccept', name: popup.from });
            this.partyState.inParty = true;
            this.partyState.waitingInHarbor = true;
            if (S.zone) {
              this.log('PARTY', 'Leaving zone to Harbor to regroup with crew.');
              window.__send({ t: 'leaveZone' });
            }
            break;
          }

          if (popup.kind === 'vote' && !popup.answered && !popup.mine) {
            popup.answered = true;
            const targetName = popup.mode ? `Challenge Mode: ${popup.mode}` : `Zone ${popup.zone}`;
            this.log('PARTY', `Party area vote for ${targetName}. Voting YES!`);
            window.__send({ t: 'vote', yes: true });
            break;
          }
        }
      }

      // Check if party disbanded
      if (this.partyState.inParty && !isCurrentlyInParty) {
        this.log('PARTY', 'Party disbanded or kicked from crew. Resuming independent solo grind!');
        this.partyState.inParty = false;
        this.partyState.waitingInHarbor = false;

        const bestZoneId = this.getBestZone();
        const zoneDef = S.defs?.zones ? S.defs.zones[bestZoneId] : null;
        this.log('ZONE_CHANGE', `Setting sail solo into Zone ${bestZoneId}: ${zoneDef?.name || 'Open Waters'}`);
        window.__send({ t: 'enter', zone: bestZoneId });
      } else if (isCurrentlyInParty) {
        this.partyState.inParty = true;
      }
    },

    // --------------------------------------------------
    // GEMINI 3.5 FLASH LITE AI AUTONOMOUS MIND & CHAT
    // --------------------------------------------------
    async handleIncomingChat(raw, force = false) {
      if (!this.config.geminiChat || !this.geminiConfig.enabled) return;
      const msg = raw?.m || raw;
      if (!msg || !msg.text || !msg.from) return;

      const myName = window.__S?.me?.name || 'cuang-bot';
      if (msg.from.toLowerCase() === myName.toLowerCase()) return;

      const now = Date.now();
      const elapsed = now - this.geminiConfig.lastReplyTime;
      if (!force && elapsed < this.geminiConfig.cooldownMs) {
        return;
      }

      const ch = msg.ch || 'global';
      this.geminiConfig.lastReplyTime = now;
      this.log('CHAT', `Incoming [${ch.toUpperCase()}] from ${msg.from}: "${msg.text}"`);

      try {
        const S = window.__S;
        const me = S?.me;
        const currentZoneId = S?.zone?.zone ?? 0;
        const zoneName = currentZoneId === 0 ? 'Harbor' : (S?.defs?.zones?.[currentZoneId]?.name || `Zone ${currentZoneId}`);
        const bestZoneId = this.getBestZone();
        const equippedWep = me?.items?.find(i => i.uid === me.equipped);
        const equippedWepDef = S?.defs?.weapons?.find(w => w.id === equippedWep?.w);
        const boatDef = S?.defs?.boats?.find(b => b.id === me?.boat);
        const helperDef = S?.defs?.helpers?.find(h => h.id === me?.helper);

        const stateSummary = [
          `Captain Status:`,
          `- Name: ${me?.name || 'cuang-bot'}`,
          `- Level: ${me?.level || 1} (XP: ${Math.round(me?.xp || 0)})`,
          `- Coins: ${(me?.coins || 0).toLocaleString()}`,
          `- Unspent Skill Points: ${me?.points || 0}`,
          `- Current Skills (max 30 each): atk=${me?.skills?.atk || 0}, aspd=${me?.skills?.aspd || 0}, crit=${me?.skills?.crit || 0}, regen=${me?.skills?.regen || 0}, hp=${me?.skills?.hp || 0}, def=${me?.skills?.def || 0}`,
          `- Skill Priority for auto-allocation: ${this.config.skillPriority.join(' > ')}`,
          `- Current Zone: ${currentZoneId} (${zoneName}) | Highest Unlocked Zone: ${bestZoneId}`,
          `- Vessel: ${boatDef?.name || me?.boat || 'Raft'} (HP: ${boatDef?.hp || 100}, DEF: ${boatDef?.def || 0})`,
          `- Crew Helper: ${helperDef?.name || me?.helper || 'None'}`,
          `- Equipped Blaster: ${equippedWepDef?.name || equippedWep?.w || 'None'} (Tier ${equippedWepDef?.tier || 0}, DPS: ${equippedWepDef?.dps || 0})`,
          `- Bag Inventory: ${(me?.items || []).map(i => `${i.w}${i.uid === me?.equipped ? ' [EQUIPPED]' : ''}`).join(', ')}`,
          `- Combat Stats: Combo=${S?.combo || 0} (Best: ${this.stats.bestCombo}), Deflected boss shots=${this.stats.shotsIntercepted}, Kills=${this.stats.killsThisSession}`,
          `- Best Market Deal Radar: ${this.bestMarketDeal ? `${this.bestMarketDeal.wDef.name} for ${this.bestMarketDeal.listing.price.toLocaleString()}c (${this.bestMarketDeal.discountPct}% off)` : 'None'}`,
          `- Saving Budget Target: ${this.savingGoal ? `${this.savingGoal.toLocaleString()} coins` : 'None'}`
        ].join('\n');

        const recentLogsText = (this.logs.slice(0, 15)).map(l => `[${l.timeStr}] [${l.type}] ${l.message}`).join('\n');

        const systemPrompt = `You are the autonomous AI Captain Mind of cuang-bot, a fearless and witty pirate sailing the high seas in the multiplayer ocean RPG Reefblast.

SYSTEM KNOWLEDGE & GAME MECHANICS:
1. Skills (Max 30 points each):
   - 'atk' (Firepower): Increases blast projectile damage.
   - 'aspd' (Quick Trigger): Increases attack rate / fire speed.
   - 'crit' (Deadly Aim): Increases critical strike chance and multiplier.
   - 'regen' (Patch Hull): Passive vessel hull repair per second.
   - 'hp' (Reinforced Plating): Increases maximum vessel durability/hit points.
   - 'def' (Ironclad): Flat armor damage reduction against enemy and boss shots.
2. Skill Priorities:
   - When leveling up, the bot auto-allocates unspent skill points according to 'config.skillPriority'. You can change this priority array at any time with 'set_skill_priority'.
3. Combat & Navigation:
   - Zones range from 0 (Harbor safe hub) to 19 (deep sea combat areas).
   - In combat, the bot automatically aims, predicts enemy fish movement, intercepts incoming boss torpedoes, and maintains combos with emergency low-HP kills.
   - You can order the ship to navigate to any zone with 'set_zone' (use 0 for Harbor).
4. Harbor Economy & Upgrades:
   - Shipyard: Buy and equip stronger vessels ('buy_boat').
   - Tavern: Recruit crew helpers ('hire_helper').
   - Forge: Craft weapons for the current zone tier ('craft_weapon').
   - Market: Scans for items at >= 25% discount, snipes high-tier blasters, and auto-salvages junk weapons.
5. Logs & History:
   - You have direct access to recent event logs. When asked about past actions, status, combo records, recent level-ups, or what the bot has been doing, consult the logs and status to give an accurate, truthful answer.

YOUR CAPABILITIES & ACTIONS:
- You control the bot via the 'actions' array in your JSON output. Supported actions:
  * allocate_skill: { "action": "allocate_skill", "skill": "atk"|"aspd"|"crit"|"regen"|"hp"|"def", "points": <number> }
  * set_skill_priority: { "action": "set_skill_priority", "skillPriority": ["crit", "aspd", "atk", ...] }
  * set_zone: { "action": "set_zone", "zoneId": <0-19> }
  * equip_weapon: { "action": "equip_weapon", "weaponId": "<uid or weapon id>" }
  * craft_weapon: { "action": "craft_weapon", "weaponId": "<weapon id>" }
  * buy_boat: { "action": "buy_boat", "boatId": "<boat id>" }
  * hire_helper: { "action": "hire_helper", "helperId": "<helper id>" }
  * toggle_bot: { "action": "toggle_bot", "running": true|false }
  * toggle_feature: { "action": "toggle_feature", "feature": "<featureName>", "enabled": true|false }
  * leave_party: { "action": "leave_party" }
  * read_logs: { "action": "read_logs" }
- If a message asks to change skills, change zones, or perform an action, generate the matching action in 'actions'.
- If no action is needed (just chatting), return an empty array [] for 'actions'.
- In 'reply', write a short, lively, pirate-styled response (8 to 25 words max, no quotes, no markdown, no emojis). Always stay in character as Captain cuang-bot.`;

        const userPrompt = `${stateSummary}\n\nRecent Event Logs (newest first):\n${recentLogsText || 'No logs recorded yet.'}\n\nIncoming Message from "${msg.from}" in [${ch.toUpperCase()}]:\n"${msg.text}"`;

        const url = `https://generativelanguage.googleapis.com/v1beta/models/${this.geminiConfig.model}:generateContent?key=${this.geminiConfig.apiKey}`;
        const res = await fetch(url, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            systemInstruction: {
              parts: [{ text: systemPrompt }]
            },
            contents: [{
              parts: [{ text: userPrompt }]
            }],
            generationConfig: {
              temperature: 0.7,
              maxOutputTokens: 350,
              responseMimeType: "application/json",
              responseSchema: {
                type: "OBJECT",
                properties: {
                  reply: {
                    type: "STRING",
                    description: "Pirate reply to send to the chat (8-25 words max, in character, referencing logs/stats if asked, no quotes, no markdown, no emojis)."
                  },
                  actions: {
                    type: "ARRAY",
                    description: "List of bot control actions to execute.",
                    items: {
                      type: "OBJECT",
                      properties: {
                        action: {
                          type: "STRING",
                          enum: [
                            "allocate_skill",
                            "set_skill_priority",
                            "set_zone",
                            "equip_weapon",
                            "craft_weapon",
                            "buy_boat",
                            "hire_helper",
                            "toggle_bot",
                            "toggle_feature",
                            "leave_party",
                            "read_logs"
                          ]
                        },
                        skill: { type: "STRING", enum: ["atk", "aspd", "crit", "regen", "hp", "def"] },
                        points: { type: "INTEGER" },
                        skillPriority: { type: "ARRAY", items: { type: "STRING" } },
                        zoneId: { type: "INTEGER" },
                        weaponId: { type: "STRING" },
                        boatId: { type: "STRING" },
                        helperId: { type: "STRING" },
                        running: { type: "BOOLEAN" },
                        feature: { type: "STRING" },
                        enabled: { type: "BOOLEAN" }
                      },
                      "required": ["action"]
                    }
                  }
                },
                required: ["reply", "actions"]
              }
            }
          })
        });

        if (!res.ok) {
          const errText = await res.text();
          this.log('CHAT', `Gemini API error ${res.status}: ${errText.slice(0, 120)}`);
          return;
        }

        const data = await res.json();
        let rawContent = data.candidates?.[0]?.content?.parts?.[0]?.text?.trim() || '{}';
        if (rawContent.startsWith('```json')) rawContent = rawContent.replace(/^```json\s*/, '').replace(/\s*```$/, '');
        else if (rawContent.startsWith('```')) rawContent = rawContent.replace(/^```\s*/, '').replace(/\s*```$/, '');

        let parsed = null;
        try {
          parsed = JSON.parse(rawContent);
        } catch (pe) {
          this.log('CHAT', `Failed to parse Gemini JSON: ${pe.message}`);
          return;
        }

        let reply = parsed.reply || '';
        reply = reply.replace(/["*]/g, '').replace(/\n+/g, ' ').trim();
        if (reply.length > 150) reply = reply.slice(0, 147) + '...';

        if (Array.isArray(parsed.actions) && parsed.actions.length > 0) {
          for (const act of parsed.actions) {
            this.executeGeminiAction(act);
          }
        }

        if (reply) {
          this.log('CHAT', `Gemini 3.5 Flash Lite reply: "${reply}" -> Sending to ${ch.toUpperCase()}...`);
          if (ch === 'party') {
            window.__send({ t: 'chat', id: 'party', text: reply });
          } else if (ch === 'dm') {
            window.__send({ t: 'chat', id: 'dm', name: msg.from, text: reply });
          } else {
            window.__send({ t: 'chat', id: 'global', text: reply });
          }
          this.log('CHAT', `Successfully sent message to ${ch.toUpperCase()}: "${reply}"`);
        }
      } catch (err) {
        this.log('CHAT', `Failed to process Gemini chat: ${err.message}`);
      }
    },

    executeGeminiAction(act) {
      if (!act || !act.action) return;
      const S = window.__S;
      const world = window.__world;
      const me = S?.me;
      this.log('GEMINI', `[Gemini Action] Executing: ${act.action}`, act);

      switch (act.action) {
        case 'allocate_skill': {
          const validSkills = ['atk', 'aspd', 'crit', 'regen', 'hp', 'def'];
          const skill = act.skill?.toLowerCase();
          if (!validSkills.includes(skill)) return;
          const available = me?.points || 0;
          if (available <= 0) return;
          const maxPerSkill = S?.defs?.skillMax || 30;
          const current = (me?.skills && me.skills[skill]) || 0;
          const pointsWanted = Math.max(1, act.points || 1);
          const pointsToAdd = Math.min(pointsWanted, available, maxPerSkill - current);
          if (pointsToAdd <= 0) return;
          window.__send({ t: 'skill', id: skill, n: pointsToAdd });
          this.log('SKILL', `[Gemini] Allocated ${pointsToAdd} pt(s) to ${skill.toUpperCase()} (${current + pointsToAdd}/${maxPerSkill})`);
          break;
        }

        case 'set_skill_priority': {
          if (Array.isArray(act.skillPriority) && act.skillPriority.length > 0) {
            const valid = ['atk', 'aspd', 'crit', 'regen', 'hp', 'def'];
            const newPrio = act.skillPriority.map(s => s.toLowerCase()).filter(s => valid.includes(s));
            valid.forEach(s => { if (!newPrio.includes(s)) newPrio.push(s); });
            this.config.skillPriority = newPrio;
            this.log('SYSTEM', `[Gemini] Updated skill priority order: ${newPrio.join(' > ')}`);
          }
          break;
        }

        case 'set_zone': {
          const z = act.zoneId;
          if (typeof z === 'number' && z >= 0) {
            if (z === 0) {
              this.log('ZONE_CHANGE', `[Gemini] Returning vessel to Harbor.`);
              window.__send({ t: 'leaveZone' });
            } else {
              const zDef = S?.defs?.zones?.[z];
              const zName = zDef?.name || `Zone ${z}`;
              this.log('ZONE_CHANGE', `[Gemini] Sailing vessel into Zone ${z}: ${zName}`);
              window.__send({ t: 'enter', zone: z });
            }
          }
          break;
        }

        case 'equip_weapon': {
          if (!me?.items || !act.weaponId) return;
          const target = act.weaponId.toLowerCase();
          const it = me.items.find(i => i.uid === act.weaponId || i.w?.toLowerCase() === target);
          if (it) {
            const wDef = S?.defs?.weapons?.find(w => w.id === it.w);
            window.__send({ t: 'equipWeapon', id: it.uid });
            this.log('UPGRADE', `[Gemini] Equipped weapon: ${wDef?.name || it.w} (UID: ${it.uid})`);
          }
          break;
        }

        case 'craft_weapon': {
          if (!act.weaponId) return;
          const targetWep = S?.defs?.weapons?.find(w => w.id === act.weaponId || w.name.toLowerCase() === act.weaponId.toLowerCase());
          if (targetWep) {
            if (world?.me) { world.me.x = -26; world.me.z = -12; }
            window.__send({ t: 'mv', x: -26, z: -12, r: 0 });
            window.__send({ t: 'craft', id: targetWep.id });
            this.log('FORGE', `[Gemini] Ordered Forge crafting: ${targetWep.name} for ${targetWep.cost} coins.`);
          }
          break;
        }

        case 'buy_boat': {
          if (!act.boatId) return;
          const targetBoat = S?.defs?.boats?.find(b => b.id === act.boatId || b.name.toLowerCase() === act.boatId.toLowerCase());
          if (targetBoat) {
            if (world?.me) { world.me.x = 26; world.me.z = -12; }
            window.__send({ t: 'mv', x: 26, z: -12, r: 0 });
            window.__send({ t: 'buyBoat', id: targetBoat.id });
            window.__send({ t: 'equipBoat', id: targetBoat.id });
            this.log('UPGRADE', `[Gemini] Purchased and equipped boat: ${targetBoat.name}`);
          }
          break;
        }

        case 'hire_helper': {
          if (!act.helperId) return;
          const targetHelper = S?.defs?.helpers?.find(h => h.id === act.helperId || h.name.toLowerCase() === act.helperId.toLowerCase());
          if (targetHelper) {
            if (world?.me) { world.me.x = -9; world.me.z = -6; }
            window.__send({ t: 'mv', x: -9, z: -6, r: 0 });
            window.__send({ t: 'buyHelper', id: targetHelper.id });
            window.__send({ t: 'equipHelper', id: targetHelper.id });
            this.log('TAVERN', `[Gemini] Recruited tavern helper: ${targetHelper.name}`);
          }
          break;
        }

        case 'toggle_bot': {
          if (typeof act.running === 'boolean') {
            if (act.running && !this.running) this.start();
            else if (!act.running && this.running) this.stop();
          }
          break;
        }

        case 'toggle_feature': {
          if (act.feature && typeof act.enabled === 'boolean') {
            if (this.config.hasOwnProperty(act.feature)) {
              this.config[act.feature] = act.enabled;
              this.log('SYSTEM', `[Gemini] Set automation feature "${act.feature}" to ${act.enabled}.`);
            }
          }
          break;
        }

        case 'leave_party': {
          window.__send({ t: 'partyLeave' });
          this.partyState.inParty = false;
          this.partyState.waitingInHarbor = false;
          this.log('PARTY', `[Gemini] Left current party crew.`);
          break;
        }

        case 'read_logs': {
          this.log('SYSTEM', `[Gemini] Read and reported recent event logs.`);
          break;
        }
      }
    },

    testChat(text = 'Ahoy cuang-bot!', from = 'CaptainTester', ch = 'global', force = true) {
      this.handleIncomingChat({ text, from, ch, at: Date.now() }, force);
    },

    // --------------------------------------------------
    // COMBAT & COMBO PRESERVATION ENGINE
    // --------------------------------------------------
    combatStep() {
      const S = window.__S;
      const world = window.__world;
      if (!S || !world || !this.running) return;

      if (S.combo > this.stats.bestCombo) this.stats.bestCombo = S.combo;

      // Modal handling (reconnection & dismissing popups that pause fishing)
      const modal = document.getElementById('modal');
      if (modal && !modal.classList.contains('hidden')) {
        const recBtn = modal.querySelector('[data-act="reconnect"]');
        if (recBtn) {
          const now = Date.now();
          if (S?.kicked) {
            if (!this._lastKickLog || now - this._lastKickLog > 15000) {
              this.log('SYSTEM', 'Signed in elsewhere. Waiting before reconnecting...');
              this._lastKickLog = now;
            }
            if (!this._kickWaitStart) this._kickWaitStart = now;
            if (now - this._kickWaitStart > 25000) {
              this._kickWaitStart = null;
              this.log('SYSTEM', 'Attempting reconnection to game server...');
              recBtn.click();
            }
            return;
          }
          this.log('SYSTEM', 'Connection lost modal detected. Reconnecting...');
          recBtn.click();
          return;
        }

        // Dismiss Zone Clear modal so grinding continues without user intervention
        const stayBtn = modal.querySelector('[data-act="zcStay"]');
        const closeBtn = modal.querySelector('[data-close]');
        const contBtn = modal.querySelector('button.brass');
        if (stayBtn) { this.log('SYSTEM', 'Dismissed Zone Clear modal (stayed fishing)'); stayBtn.click(); }
        else if (closeBtn) closeBtn.click();
        else if (contBtn) contBtn.click();
      }

      const reveal = document.getElementById('reveal');
      if (reveal && !reveal.classList.contains('hidden')) reveal.classList.add('hidden');

      if (!S.zone) return;
      this._lastActiveZone = S.zone.zone;
      if (!S.autofire) S.autofire = true;

      // DEFENSE 1: Intercept incoming boss torpedoes to save boat HP and combo
      if (this.config.defendIncomingShots && world.shots && world.shots.size > 0) {
        const dangerous = Array.from(world.shots.values()).filter(s => s.z > 0.5);
        if (dangerous.length > 0) {
          dangerous.sort((a, b) => b.z - a.z);
          const s = dangerous[0];
          const origin = world.origin ? world.origin(S.slot) : { x: 0, z: 7.3 };
          const curX = s.x ?? s.tx ?? 0;
          const curZ = s.z ?? s.tz ?? 0;
          const dist = Math.hypot(curX - origin.x, curZ - origin.z);
          const leadTime = Math.min(0.35, dist / 22);
          S.aim = { 
            x: curX + (s.vx || 0) * leadTime, 
            z: curZ + (s.vz || 0) * leadTime 
          };
          this.stats.shotsIntercepted++;
          this.updateUI('Intercepting enemy projectile');
          return;
        }
      }

      // OFFENSE: Target selection & Aiming
      if (!world.fishes || world.fishes.size === 0) {
        this.updateUI('Awaiting fish spawns...');
        return;
      }

      const validFishes = Array.from(world.fishes.values()).filter(f => {
        if (f.dying || f.gone) return false;
        if (f.flags & 8) return false; // Warded
        if (f.flags & 1) return false; // Shell
        return true;
      });

      if (!validFishes.length) {
        this.updateUI('Scanning for vulnerable fish...');
        return;
      }

      const origin = world.origin ? world.origin(S.slot) : { x: 0, z: 7.3 };
      const eqItem = S.me?.items?.find(i => i.uid === S.me.equipped);
      const eqDef = S.defs?.weapons?.find(w => w.id === eqItem?.w);
      const isHoming = eqDef?.kind === 'homing';

      // COMBO SAVER: If combo timer is critically low (< 0.85s) and we have an active combo (>= 3),
      // instantly target the lowest HP fish in reach to secure an immediate kill and refresh the timer to 2.0s!
      const comboCritical = S.combo >= 3 && (S.comboT !== undefined && S.comboT < this.config.comboSaverThreshold);

      let target = null;
      if (comboCritical) {
        // Prioritize rapid kill
        validFishes.sort((a, b) => {
          const distA = Math.hypot(a.x - origin.x, a.z - origin.z);
          const distB = Math.hypot(b.x - origin.x, b.z - origin.z);
          return (a.hp * 2 + distA) - (b.hp * 2 + distB);
        });
        target = validFishes[0];
      } else {
        // Normal priority: Mini-bosses and Bosses first for high XP/coins, then closest
        validFishes.sort((a, b) => {
          const aBoss = (a.d?.boss || a.d?.mini) ? 1 : 0;
          const bBoss = (b.d?.boss || b.d?.mini) ? 1 : 0;
          if (aBoss !== bBoss) return bBoss - aBoss;
          const distA = Math.hypot(a.x - origin.x, a.z - origin.z);
          const distB = Math.hypot(b.x - origin.x, b.z - origin.z);
          return (a.hp - b.hp) || (distA - distB);
        });
        target = validFishes[0];
      }

      let aimX = target.x;
      let aimZ = target.z;

      if (!isHoming && this.config.leadTargeting) {
        const bulletSpeed = eqDef?.speed || 25;
        const dist = Math.hypot(target.x - origin.x, target.z - origin.z);
        const flightTime = dist / bulletSpeed;
        aimX += (target.vx || 0) * flightTime;
        aimZ += (target.vz || 0) * flightTime;
      }

      aimX = Math.max(-16.5, Math.min(16.5, aimX));
      aimZ = Math.max(-10.5, Math.min(4.0, aimZ));

      S.aim = { x: aimX, z: aimZ };
      const fishName = target.d?.title || target.d?.name || 'Fish';
      this.updateUI(comboCritical ? `⚡ Combo Saver: ${fishName}` : `Targeting: ${fishName} (${Math.round(target.hp / 10)}% HP)`);
    },

    // --------------------------------------------------
    // PROGRESSION MANAGER STEP
    // --------------------------------------------------
    async managerStep() {
      if (this._inManager) return;
      this._inManager = true;
      try {
        const S = window.__S;
        if (!S || !S.me || !this.running) return;
        if (!S.ws || S.ws.readyState !== 1) return;
        const me = S.me;

        // 0. Event listener & party handling
        this.hookGameEvents();
        this.handlePartyManagement();

        if (this.partyState.inParty && this.partyState.waitingInHarbor && !S.zone) {
          return;
        }

        // 0.1 Auto-Equip Best Weapon in Inventory
        this.autoEquipBestWeapon();

        // 1. Level up check
        if (me.level > this._lastLevel) {
          const gained = me.level - this._lastLevel;
          this.log('LEVEL_UP', `Leveled up to Level ${me.level}! (+${gained} skill point${gained > 1 ? 's' : ''})`, { level: me.level });
          this._lastLevel = me.level;
        }

        // 2. Auto-allocate skill points with mathematical optimal ratio (AM-GM Offensive + EHP Defensive)
        if (this.config.autoSkills && me.points > 0) {
          const maxPerSkill = S.defs?.skillMax || 30;
          const ratios = { atk: 0.35, aspd: 0.30, def: 0.20, hp: 0.10, regen: 0.05 };
          const regenCap = 5;

          while (me.points > 0) {
            let bestStat = null;
            let lowestScore = Infinity;

            for (const [stat, weight] of Object.entries(ratios)) {
              const current = (me.skills && me.skills[stat]) || 0;
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

            const curVal = (me.skills && me.skills[bestStat]) || 0;
            const cap = bestStat === 'regen' ? regenCap : maxPerSkill;
            const pointsToSpend = Math.min(me.points, cap - curVal, 1);
            if (pointsToSpend <= 0) break;

            window.__send({ t: 'skill', id: bestStat, n: pointsToSpend });
            if (!me.skills) me.skills = {};
            me.skills[bestStat] = curVal + pointsToSpend;
            me.points -= pointsToSpend;
            this.log('SKILL', `Allocated skill point to: ${bestStat.toUpperCase()} (${curVal + pointsToSpend}/${cap}) [Optimal Ratio]`, { skill: bestStat });
          }
        }

        // 3. Auto-claim daily quests
        if (this.config.autoQuests && me.daily?.list && !this._claimPending) {
          const readyQuests = me.daily.list.filter(q => q.prog >= q.target && !q.claimed);
          if (readyQuests.length > 0) {
            this._claimPending = true;
            for (const q of readyQuests) {
              this.log('QUEST', `Claiming daily quest: "${q.text}" (+${q.coins} coins, +${q.xp} XP)`, { quest: q.id });
              window.__send({ t: 'questClaim', id: q.id });
            }
            if (!me.daily.bonus && me.daily.list.every(q => q.claimed || q.prog >= q.target)) {
              window.__send({ t: 'questClaim', id: 'bonus' });
              this.log('QUEST', `Claiming daily bonus reward! (+${me.daily.bonusCoins} coins, +${me.daily.bonusXP} XP)`);
            }
            setTimeout(() => { this._claimPending = false; }, 3000);
          }
        }

        // 4. Harbor Progression (Upgrading gear, market sniping, crafting, salvaging)
        if (!S.zone) {
          await this.handleHarborProgression();
          return;
        }

        // 5. Zone Progression Check (advance solo if not in party and unlocked higher safe zone)
        const bestZoneId = this.getBestZone();
        const currentZoneId = S.zone?.zone;
        if (this.config.autoZoneAdvance && !this.partyState.inParty && currentZoneId !== undefined && bestZoneId > currentZoneId) {
          const newZoneDef = S.defs.zones[bestZoneId];
          this.log('ZONE_CHANGE', `Advancing from Zone ${currentZoneId} to Zone ${bestZoneId} (${newZoneDef?.name || 'Higher Tier'})!`, { from: currentZoneId, to: bestZoneId });
          window.__send({ t: 'leaveZone' });
        }
      } finally {
        this._inManager = false;
      }
    },

    calcWeaponScore(wDef, item) {
      const S = window.__S;
      if (!wDef) return -1;
      const isExclusive = !!wDef.exclusive;
      const playerLevel = S?.me?.level || 1;
      const effTier = isExclusive ? 6.5 : (wDef.tier || 0);

      let baseDps = wDef.dps || 0;
      if (wDef.scales && S?.defs?.tierDps && S?.defs?.exclusiveEdge) {
        const t = Math.min(6, Math.floor(playerLevel / 16) + 1);
        baseDps = (S.defs.tierDps[t] || 10945) * (S.defs.exclusiveEdge || 1.08);
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
      if (traits.has('lifesteal')) traitMult += 0.12;
      if (traits.has('poison')) traitMult += 0.10;
      if (traits.has('burn')) traitMult += 0.08;
      if (traits.has('pierce')) traitMult += 0.08;

      const plBonus = (wDef.pl || 0) * 0.1;
      return (effTier * 2000) + (effectiveDps * traitMult) + plBonus;
    },

    calcBoatScore(bDef) {
      if (!bDef) return -1;
      const isExclusive = !!bDef.exclusive;
      const effTier = isExclusive ? 6.5 : (bDef.tier || 0);

      const baseHp = bDef.hp || 100;
      const baseDef = bDef.def || 0;
      const hpBuff = 1 + (bDef.buffs?.hp || 0);
      const defBuff = 1 + (bDef.buffs?.def || 0);
      const effectiveHp = baseHp * hpBuff;
      const effectiveDef = baseDef * defBuff;
      const ehp = effectiveHp * (1 + effectiveDef / 80);

      const dmgBuff = (bDef.buffs?.dmg || 0) * 40000;
      const aspdBuff = (bDef.buffs?.aspd || 0) * 35000;
      const critBuff = (bDef.buffs?.crit || 0) * 30000;
      const regenScore = (bDef.buffs?.regen || 0) * 2000000;
      const econScore = ((bDef.buffs?.coin || 0) + (bDef.buffs?.xp || 0)) * 10000;
      const plScore = (bDef.pl || 0) * 2;

      return (effTier * 20000) + (ehp * 0.5) + dmgBuff + aspdBuff + critBuff + regenScore + econScore + plScore;
    },

    autoEquipBestWeapon() {
      const S = window.__S;
      const me = S?.me;
      if (!me?.items || me.items.length === 0) return;

      let bestWeaponItem = null;
      let bestScore = -1;

      for (const item of me.items) {
        const wDef = S.defs?.weapons?.find(w => w.id === item.w);
        if (!wDef) continue;
        const score = this.calcWeaponScore(wDef, item);
        if (score > bestScore) {
          bestScore = score;
          bestWeaponItem = { item, wDef, score };
        }
      }

      if (bestWeaponItem && bestWeaponItem.item.uid !== me.equipped) {
        if (S.zone) {
          // Cannot equip weapons while in combat zone; safely return to Harbor to equip
          this.log('UPGRADE', `Superior weapon in bag (${bestWeaponItem.wDef.name}, Tier ${bestWeaponItem.wDef.tier || 0}, PL ${bestWeaponItem.wDef.pl || 0}). Returning to Harbor to equip!`);
          window.__send({ t: 'leaveZone' });
          return;
        }
        const b = bestWeaponItem;
        window.__send({ t: 'equipWeapon', id: b.item.uid });
        this._zoneDeaths = {};
        this._lastDeathTime = null;
        this.log('UPGRADE', `Equipped best weapon in inventory: ${b.wDef.name} (Tier ${b.wDef.tier || 0}, DPS: ${b.wDef.dps || 0}, PL: ${b.wDef.pl || 0})`, { uid: b.item.uid });
      }
    },

    getBestZone() {
      const S = window.__S;
      if (!S || !S.defs || !S.defs.zones) return 0;
      const level = S.me?.level || 1;
      const unlocked = S.defs.zones.filter(z => level >= z.level);
      if (!unlocked.length) return 0;

      const highestUnlockedId = unlocked[unlocked.length - 1].id;
      const me = S.me;
      const bDef = S.defs?.boats?.find(b => b.id === me?.boat);
      const boatHp = bDef?.hp || 100;
      const eqItem = me?.items?.find(i => i.uid === me?.equipped);
      const wDef = S.defs?.weapons?.find(w => w.id === eqItem?.w);
      const wepTier = wDef?.tier || 0;

      this._zoneDeaths = this._zoneDeaths || {};

      // Reset death history after 3 minutes of safe grinding
      if (this._lastDeathTime && Date.now() - this._lastDeathTime > 3 * 60 * 1000) {
        this._zoneDeaths = {};
        this._lastDeathTime = null;
      }

      // High-tier vessels like Red Dart (430 HP, 69 DEF) should grind in Zone 8+
      const minVesselFloor = boatHp >= 400 ? 8 : (boatHp >= 200 ? 5 : 1);

      // Check downwards from highest unlocked zone
      for (let i = unlocked.length - 1; i >= 0; i--) {
        const zId = unlocked[i].id;
        const deaths = this._zoneDeaths[zId] || 0;
        
        // If died 2+ times in this zone recently:
        if (deaths >= 2) {
          continue;
        }

        // Zone 9+ (Bone Graveyard, Lv 43+) requires at least a Tier 7 weapon (Storm Coil / Rift Seeker)
        // Fighting Zone 9 with Tier 3 weapon (64 DPS) causes boat to get overwhelmed
        if (zId >= 9 && wepTier < 7) {
          continue;
        }

        return Math.max(zId, minVesselFloor);
      }
      return minVesselFloor;
    },

    // --------------------------------------------------
    // MARKET DEAL SCANNER & SNIPER ENGINE
    // --------------------------------------------------
    pollMarket() {
      if (!window.__S || !this.running) return;
      window.__send({ t: 'mkList' });
      this._lastMarketPoll = Date.now();
      setTimeout(() => this.analyzeMarketDeals(), 500);
    },

    calcItemQuality(it) {
      const S = window.__S;
      if (!S?.defs?.rolls) return 50;
      const R = S.defs.rolls;
      const barOf = (v, r) => (v - r.min) / (r.max - r.min);
      return Math.round((barOf(it.atk, R[0]) + barOf(it.aspd, R[1]) + barOf(it.crit, R[2])) / 3 * 100);
    },

    calcFairValue(it) {
      const S = window.__S;
      const w = S?.defs?.weapons.find(x => x.id === it.w);
      if (!w || it.bound) return 0;
      const [b, sp] = S.defs.fairQ || [0.6, 0.8];
      const qual = this.calcItemQuality(it);
      const v = w.fair * (b + sp * qual / 100);
      if (v < 100) return Math.round(v);
      const mag = Math.pow(10, Math.floor(Math.log10(v)) - 1);
      return Math.round(v / mag) * mag;
    },

    analyzeMarketDeals() {
      const S = window.__S;
      const M = S?.market;
      if (!M || !M.listings || !M.listings.length) return false;

      const myCoins = S.me?.coins || 0;
      const myWeaponId = S.me?.items?.find(i => i.uid === S.me.equipped)?.w || 'popper';
      const myWeaponDef = S.defs.weapons.find(w => w.id === myWeaponId);
      const myWeaponTier = myWeaponDef?.tier ?? 0;
      const myWeaponDPS = myWeaponDef?.dps ?? 0;

      let bestOverallDeal = null;
      let bestAffordableUpgrade = null;
      let superDeal = null;

      for (const listing of M.listings) {
        if (!listing || !listing.item) continue;
        if (listing.seller.toLowerCase() === S.me.name.toLowerCase()) continue;
        
        if (this._boughtListings[listing.id]) {
          // If we actually own this weapon type, skip it
          if (S.me?.items?.some(i => i.w === listing.item.w)) {
            continue;
          }
          // Otherwise, previous buy was not finalized or was false positive, clear flag
          delete this._boughtListings[listing.id];
        }

        // If we have enough coins, forgive previous failures so we can attempt sniper
        if (this._failedMarketListings[listing.id] && myCoins >= listing.price) {
          delete this._failedMarketListings[listing.id];
        }
        if ((this._failedMarketListings[listing.id] || 0) >= 3) continue;

        const wDef = S.defs.weapons.find(w => w.id === listing.item.w);
        if (!wDef) continue;

        const quality = this.calcItemQuality(listing.item);
        const fairVal = this.calcFairValue(listing.item);
        const baselinePrice = Math.max(wDef.cost || wDef.fair, fairVal);
        const discountPct = Math.round((1 - (listing.price / baselinePrice)) * 100);

        // Genuine Upgrade condition:
        // Must be higher tier, OR significantly higher DPS with a massive discount
        const isUpgrade = wDef.tier > myWeaponTier || 
                         (wDef.tier === myWeaponTier && wDef.dps > myWeaponDPS * 1.15 && discountPct >= 30);

        const deal = { listing, wDef, quality, fairVal, discountPct };

        // Super Deal: High Tier (>= 7), huge discount (>= 35%), and realistically attainable within farming reach
        const isRealisticGoal = listing.price <= Math.max(65000, myCoins * 3);
        if (wDef.tier >= 7 && discountPct >= 35 && isRealisticGoal) {
          // Rank by tier, discount, and affordability efficiency
          const dealScore = (wDef.tier * 2000) + (discountPct * 50) + (wDef.dps || 0) - (listing.price / 100);
          if (!superDeal || dealScore > (superDeal.score || 0)) {
            superDeal = { ...deal, score: dealScore };
          }
        }

        // Radar tracking
        if (discountPct >= this.config.minMarketDiscountPct && isUpgrade) {
          if (!bestOverallDeal || discountPct > bestOverallDeal.discountPct) {
            bestOverallDeal = deal;
          }
        }

        // Affordable upgrade right now
        // BUT: if we are saving for a Super Deal (e.g. Kent's Tier 15 Rift Seeker for 28k), DO NOT get distracted by buying lower-tier intermediate weapons that drain our savings!
        if (isUpgrade && myCoins >= listing.price) {
          if (superDeal && String(superDeal.listing.id) !== String(listing.id) && wDef.tier < superDeal.wDef.tier && myCoins < superDeal.listing.price) {
            // We are saving for a superior super deal weapon! Skip buying this distraction!
            continue;
          }
          const score = (wDef.tier * 10000) + ((wDef.dps || 0) * 100) + Math.max(0, discountPct);
          if (!bestAffordableUpgrade || score > bestAffordableUpgrade.score) {
            bestAffordableUpgrade = { ...deal, score };
          }
        }
      }

      this.bestMarketDeal = bestOverallDeal;

      // If a super deal exists within realistic reach, protect savings for it
      if (superDeal) {
        this.savingGoal = superDeal.listing.price;
      } else {
        this.savingGoal = 0;
      }

      // Handle purchase
      if (this.config.autoSnipeMarketDeals && bestAffordableUpgrade) {
        const d = bestAffordableUpgrade;
        if (!S.zone) {
          return this.executeMarketBuy(d);
        } else {
          // In combat zone: only leave if combo is low (< 25) or the weapon is an extraordinary upgrade (+3 tiers)
          const currentCombo = S.combo || 0;
          const extraordinary = d.wDef.tier >= myWeaponTier + 3;
          if (currentCombo < 25 || extraordinary) {
            if ((this._failedMarketListings[d.listing.id] || 0) >= 3) return false;
            this.log('UPGRADE', `Found affordable market upgrade: ${d.wDef.name} (Tier ${d.wDef.tier}) for ${d.listing.price.toLocaleString()}c. Safely returning to Harbor to buy!`);
            window.__send({ t: 'leaveZone' });
            return false;
          }
        }
      }
      return false;
    },

    async sailTo(targetX, targetZ, stopRadius = 3.5, maxDurationMs = 6000) {
      const S = window.__S;
      const world = window.__world;
      const curBoat = S?.defs?.boats?.find(b => b.id === S?.me?.boat);
      const speed = Math.max(14, Math.min(24, (curBoat?.speed || 12) * 1.35));
      const dt = 0.1;
      const startTime = Date.now();

      while (Date.now() - startTime < maxDurationMs) {
        if (!world?.me) break;
        const dx = targetX - world.me.x;
        const dz = targetZ - world.me.z;
        const dist = Math.hypot(dx, dz);
        if (dist <= stopRadius) break;

        const wantR = Math.atan2(dx, dz);
        const curR = world.me.r || 0;
        const diffR = Math.atan2(Math.sin(wantR - curR), Math.cos(wantR - curR));
        world.me.r = curR + diffR * Math.min(1, dt * 5.5);

        const moveStep = Math.min(speed * dt, dist);
        world.me.x += Math.sin(wantR) * moveStep;
        world.me.z += Math.cos(wantR) * moveStep;

        window.__send({
          t: 'mv',
          x: +world.me.x.toFixed(2),
          z: +world.me.z.toFixed(2),
          r: +world.me.r.toFixed(2)
        });

        await this.sleep(100);
      }
    },

    async executeMarketBuy(d) {
      const S = window.__S;
      const world = window.__world;
      if (!S || !d || this._buyingMarketItem) return false;

      // Ensure we are in Harbor before attempting purchase
      if (S.zone) {
        this.log('UPGRADE', `Found affordable market upgrade: ${d.wDef.name} (Tier ${d.wDef.tier}). Returning to Harbor to buy!`);
        window.__send({ t: 'leaveZone' });
        return false;
      }

      this._buyingMarketItem = true;
      try {
        // Sail smoothly to Market NPC coordinates
        await this.sailTo(9, -4, 3.5);
        await this.sleep(150);

        // Open Market UI panel if available to trigger native rendering
        const marketNavBtn = document.querySelector('[data-open="market"]');
        if (marketNavBtn) marketNavBtn.click();
        window.__send({ t: 'mkList' });
        await this.sleep(400);

        // Verify listing still exists and coins are sufficient
        const freshListing = S.market?.listings?.find(l => String(l.id) === String(d.listing.id));
        const curCoins = S.me?.coins || 0;
        if (!freshListing || curCoins < freshListing.price) {
          this.log('UPGRADE', `Market listing ${d.listing.id} check failed (exists: ${!!freshListing}, coins: ${curCoins}/${freshListing?.price})`);
          this._failedMarketListings[d.listing.id] = (this._failedMarketListings[d.listing.id] || 0) + 1;
          return false;
        }

        this.log('UPGRADE', `SNIPING MARKET UPGRADE: Purchasing ${d.wDef.name} (Tier ${d.wDef.tier}, ${d.wDef.dps} DPS) for ${d.listing.price.toLocaleString()} coins (${d.discountPct}% off)!`, { item: d.wDef.id, price: d.listing.price });

        const preUids = new Set((S.me?.items || []).map(i => i.uid));

        // Attempt 1: Click the actual DOM button in the modal if rendered
        const domBuyBtn = document.querySelector(`[data-act="mkBuy"][data-id="${d.listing.id}"]`);
        if (domBuyBtn && !domBuyBtn.disabled) {
          domBuyBtn.click();
        } else {
          // Attempt 2: Direct WebSocket message with string ID
          window.__send({ t: 'mkBuy', id: String(d.listing.id) });
        }

        // Wait up to 3 seconds for server confirmation, checking BOTH mailbox and bag
        let acquired = false;
        for (let i = 0; i < 12; i++) {
          await this.sleep(250);
          
          // Check mailbox first: blasters bought from market arrive in me.mail!
          const mailItem = (S.me?.mail || []).find(m => m.w === d.wDef.id);
          if (mailItem) {
            this.log('UPGRADE', `Blaster ${d.wDef.name} arrived in mailbox! Claiming to inventory...`);
            window.__send({ t: 'claim' });
            await this.sleep(600);
          }

          const postItems = S.me?.items || [];
          const boughtItem = postItems.find(it => !preUids.has(it.uid) || it.w === d.wDef.id);
          if (boughtItem) {
            acquired = true;
            this._boughtListings[d.listing.id] = true;
            delete this._failedMarketListings[d.listing.id];
            this.stats.marketDealsSniped++;
            this.stats.upgradesPurchased++;
            this._zoneDeaths = {};
            this._lastDeathTime = null;
            this.log('UPGRADE', `MARKET BUY CONFIRMED: Successfully acquired ${d.wDef.name}!`, { uid: boughtItem.uid, tier: d.wDef.tier });
            this.autoEquipBestWeapon();
            break;
          }
        }

        // Close modal if open so it doesn't block the screen
        const modalEl = document.getElementById('modal');
        if (modalEl && !modalEl.classList.contains('hidden')) {
          const closeX = modalEl.querySelector('[data-close]');
          if (closeX) closeX.click();
        }

        if (!acquired) {
          this.log('UPGRADE', `Market buy sent for listing ${d.listing.id} (${d.wDef.name}), verifying inventory in next cycle.`);
        }

        // Update local market listings
        if (S.market?.listings) {
          S.market.listings = S.market.listings.filter(l => String(l.id) !== String(d.listing.id));
        }

        return acquired;
      } finally {
        this._buyingMarketItem = false;
      }
    },

    // --------------------------------------------------
    // AUTO-SALVAGE JUNK INVENTORY ENGINE
    // --------------------------------------------------
    async handleSalvageObsoleteWeapons() {
      const S = window.__S;
      const me = S?.me;
      if (!this.config.autoSalvageJunk || !me?.items || me.items.length <= 1) return;
      if (S.zone) return; // Must be in Harbor

      const equippedUid = me.equipped;
      const equippedItem = me.items.find(i => i.uid === equippedUid);
      const equippedDef = S.defs?.weapons?.find(w => w.id === equippedItem?.w);
      const equippedTier = equippedDef?.tier || 0;

      const junkUids = [];
      let estimatedCoins = 0;

      for (const item of me.items) {
        if (item.uid === equippedUid) continue;
        if (item.bound) continue; // Starter weapons can't be salvaged
        if (item.lock) continue;  // User locked weapons

        const wDef = S.defs?.weapons?.find(w => w.id === item.w);
        if (!wDef) continue;

        // Junk condition: Tier is strictly lower than equipped tier
        if (wDef.tier < equippedTier) {
          junkUids.push(item.uid);
          estimatedCoins += Math.round((wDef.cost || 0) * (S.defs?.salvage || 0.5));
        }
      }

      if (junkUids.length > 0) {
        // Unequip junk weapons from hotbar first so server allows salvage
        const cleanHotbar = (me.hotbar || []).map(u => junkUids.includes(u) ? '' : u);
        window.__send({ t: 'loadout', uids: cleanHotbar });
        await this.sleep(600);

        this.log('UPGRADE', `Liquidating ${junkUids.length} obsolete blaster(s) for ~${estimatedCoins.toLocaleString()} coins!`);
        window.__send({ t: 'salvage', uids: junkUids });
        this.stats.junkSalvaged += junkUids.length;
        this.stats.salvageCoinsEarned += estimatedCoins;
        await this.sleep(600);
      }
    },

    // --------------------------------------------------
    // HARBOR PROGRESSION & ECONOMY
    // --------------------------------------------------
    async handleHarborProgression() {
      const S = window.__S;
      const world = window.__world;
      const me = S?.me;
      if (!S || !world || !me) return;

      let performedAction = false;
      const myZone = this.getBestZone();

      // Step 0: Check mailbox for any market purchases and claim them
      if (me.mail && me.mail.length > 0) {
        this.log('UPGRADE', `Found ${me.mail.length} unclaimed blaster(s) in mailbox. Claiming to bag!`);
        window.__send({ t: 'claim' });
        await this.sleep(600);
        performedAction = true;
      }

      // Step 1: Equip best weapon in bag
      this.autoEquipBestWeapon();

      // Step 2: Auto-salvage obsolete weapons for instant coin injection
      await this.handleSalvageObsoleteWeapons();

      // Step 3: Market Procurement (Snipe best affordable high-tier weapon)
      if (this.config.autoSnipeMarketDeals) {
        await this.sailTo(9, -4, 3.5);
        window.__send({ t: 'mkList' });
        await this.sleep(350);
        const bought = await this.analyzeMarketDeals();
        if (bought) {
          performedAction = true;
          await this.sleep(800);
        }
      }

      // Re-read latest coins after potential market buy
      const currentCoins = S.me?.coins || 0;
      
      // We are saving for a market deal if savingGoal is set, OR if a valid market upgrade exists
      const hasActiveMarketTarget = this.bestMarketDeal && !this._boughtListings[this.bestMarketDeal.listing.id];
      const isSavingForSuperDeal = (this.savingGoal > 0 && currentCoins < this.savingGoal) || hasActiveMarketTarget;

      // Step 4: Shipyard (Upgrade boat for survivability; allow buying if affordable without draining core treasury)
      if (this.config.autoBuyBoats) {
        const availableBoats = S.defs.boats.filter(b => !b.retired && !b.exclusive && b.cost > 0);
        availableBoats.sort((a, b) => this.calcBoatScore(b) - this.calcBoatScore(a));

        for (const targetBoat of availableBoats) {
          const owned = me.boats && me.boats.includes(targetBoat.id);
          if (owned) {
            if (me.boat !== targetBoat.id) {
              const curBoat = S.defs.boats.find(b => b.id === me.boat);
              if (this.calcBoatScore(targetBoat) > this.calcBoatScore(curBoat)) {
                await this.sailTo(26, -9, 3.5);
                await this.sleep(150);
                window.__send({ t: 'equipBoat', id: targetBoat.id });
                this.log('UPGRADE', `Equipped vessel: ${targetBoat.name} (Tier ${targetBoat.tier || 0}, HP: ${targetBoat.hp}, DEF: ${targetBoat.def}, PL: ${targetBoat.pl || 0})`, { boat: targetBoat.id });
                performedAction = true;
              }
            }
            break;
          } else {
            const curBoat = S.defs.boats.find(b => b.id === me.boat);
            if (this.calcBoatScore(targetBoat) > this.calcBoatScore(curBoat)) {
              const canAfford = !isSavingForSuperDeal || (targetBoat.cost <= currentCoins * 0.65 && (currentCoins - targetBoat.cost) >= 80000);
              if ((S.me?.coins || 0) >= targetBoat.cost && canAfford) {
                await this.sailTo(26, -9, 3.5);
                await this.sleep(150);
                window.__send({ t: 'buyBoat', id: targetBoat.id });
                window.__send({ t: 'equipBoat', id: targetBoat.id });
                this.log('UPGRADE', `Purchased new vessel: ${targetBoat.name} (Tier ${targetBoat.tier || 0}, HP: ${targetBoat.hp}, DEF: ${targetBoat.def}, PL: ${targetBoat.pl || 0}) for ${targetBoat.cost.toLocaleString()} coins!`, { boat: targetBoat.id });
                this.stats.upgradesPurchased++;
                this._zoneDeaths = {};
                performedAction = true;
                break;
              }
            }
          }
        }
      }

      // Step 5: Tavern (Hire Helper, unless saving for super-deal)
      if (this.config.autoHireHelpers) {
        if (!isSavingForSuperDeal) {
          const availableHelpers = S.defs.helpers.filter(h => h.zone <= myZone);
          availableHelpers.sort((a, b) => (b.cost || 0) - (a.cost || 0));

          if (availableHelpers.length > 0) {
            const bestHelper = availableHelpers[0];
            const owned = me.helpers && me.helpers.includes(bestHelper.id);
            if (owned) {
              if (me.helper !== bestHelper.id) {
                await this.sailTo(-9, -4, 3.5);
                await this.sleep(150);
                window.__send({ t: 'equipHelper', id: bestHelper.id });
                this.log('TAVERN', `Assigned crew helper: ${bestHelper.name}`, { helper: bestHelper.id });
                performedAction = true;
              }
            } else if ((S.me?.coins || 0) >= bestHelper.cost) {
              await this.sailTo(-9, -4, 3.5);
              await this.sleep(150);
              window.__send({ t: 'buyHelper', id: bestHelper.id });
              window.__send({ t: 'equipHelper', id: bestHelper.id });
              this.log('TAVERN', `Hired crew helper: ${bestHelper.name} (${bestHelper.desc}) for ${bestHelper.cost.toLocaleString()} coins!`, { helper: bestHelper.id });
              this.stats.upgradesPurchased++;
              performedAction = true;
            }
          }
        }
      }

      // Step 6: Forge Crafting (Only if NO market target exists, to prevent draining treasury)
      if (this.config.autoCraftWeapons && !hasActiveMarketTarget && this.savingGoal === 0) {
        const eqItem = me.items?.find(i => i.uid === me.equipped);
        const eqDef = S.defs?.weapons?.find(w => w.id === eqItem?.w);
        const currentScore = this.calcWeaponScore(eqDef, eqItem);

        const craftable = S.defs.weapons.filter(w => !w.retired && !w.exclusive && w.cost > 0 && (S.me?.coins || 0) >= w.cost);
        craftable.sort((a, b) => this.calcWeaponScore(b, null) - this.calcWeaponScore(a, null));

        if (craftable.length > 0) {
          const targetWep = craftable[0];
          const targetScore = this.calcWeaponScore(targetWep, null);
          if (targetScore > currentScore) {
            await this.sailTo(-26, -9, 3.5);
            await this.sleep(150);
            window.__send({ t: 'craft', id: targetWep.id, n: 1 });
            this.log('FORGE', `Crafted weapon at Forge: ${targetWep.name} (Tier ${targetWep.tier || 0}, ${targetWep.dps} DPS, PL ${targetWep.pl || 0}) for ${targetWep.cost.toLocaleString()} coins!`, { item: targetWep.id });
            this.stats.upgradesPurchased++;
            this._zoneDeaths = {};
            performedAction = true;
            await this.sleep(1000);
            this.autoEquipBestWeapon();
          }
        }
      }

      if (performedAction) await this.sleep(400);

      // Step 7: Sail to Best Grinding Zone
      const targetZoneId = this.getBestZone();
      const zoneDef = S.defs.zones[targetZoneId];
      const gate = S?.defs?.harbor?.gates?.find(g => g.zone === targetZoneId);
      if (gate) {
        this.log('ZONE_CHANGE', `Sailing towards Zone ${targetZoneId} Gate at (${gate.x}, ${gate.z})...`, { zone: targetZoneId });
        await this.sailTo(gate.x, gate.z, 5.0);
      }
      this.log('ZONE_CHANGE', `Setting sail into Zone ${targetZoneId}: ${zoneDef?.name || 'Open Waters'} (Lv ${zoneDef?.level || 1}+)`, { zone: targetZoneId });
      window.__send({ t: 'enter', zone: targetZoneId });
      await this.sleep(1000);
    },

    sleep(ms) {
      return new Promise(r => setTimeout(r, ms));
    },

    // --------------------------------------------------
    // ON-SCREEN UI & LOG VIEWER
    // --------------------------------------------------
    createUI() {
      if (!document.body || document.getElementById('rb-progression-widget')) return;

      const widget = document.createElement('div');
      widget.id = 'rb-progression-widget';
      widget.style.cssText = 'position:fixed;bottom:75px;right:20px;width:340px;background:rgba(10,18,32,0.95);border:1px solid #1f6aa0;border-radius:12px;box-shadow:0 10px 40px rgba(0,0,0,0.7);color:#d8eeff;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,sans-serif;font-size:12px;z-index:999999;backdrop-filter:blur(10px);user-select:none;overflow:hidden;';

      widget.innerHTML = `
        <div id="rb-header" style="background:linear-gradient(90deg,#10345a,#1a4f82);padding:10px 14px;font-weight:bold;display:flex;justify-content:space-between;align-items:center;cursor:move;border-bottom:1px solid #23588f;">
          <span>⚓ Reefblast Automation Pro</span>
          <div style="display:flex;gap:6px;align-items:center;">
            <button id="rb-btn-export" style="background:#0c2038;border:1px solid #2a5a8a;color:#9bbcd6;padding:2px 6px;border-radius:4px;font-size:10px;cursor:pointer;">JSON</button>
            <span style="font-size:10px;color:#7cb8e8;background:#0c2038;padding:2px 6px;border-radius:4px;">F2 Toggle</span>
          </div>
        </div>
        <div style="padding:12px;">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;">
            <span style="color:#8bb2d6;">Bot Status:</span>
            <b id="rb-status-badge" style="color:#ff5555;background:rgba(255,85,85,0.15);padding:2px 8px;border-radius:4px;">STOPPED</b>
          </div>
          <div id="rb-action-display" style="font-size:11px;color:#6fd8ff;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-bottom:8px;background:rgba(0,0,0,0.25);padding:4px 8px;border-radius:4px;">Idle</div>
          
          <div style="background:rgba(0,0,0,0.35);border-radius:8px;padding:8px 10px;margin-bottom:8px;font-size:11px;line-height:1.7;">
            <div style="display:flex;justify-content:space-between;">
              <span style="color:#8bb2d6;">Captain / Level:</span>
              <b id="rb-stat-captain" style="color:#ffffff;">-</b>
            </div>
            <div style="display:flex;justify-content:space-between;">
              <span style="color:#8bb2d6;">Coins Gained:</span>
              <b id="rb-stat-coins" style="color:#ffd84a;">+0</b>
            </div>
            <div style="display:flex;justify-content:space-between;">
              <span style="color:#8bb2d6;">Active Zone:</span>
              <span id="rb-stat-zone" style="color:#00d2ff;">-</span>
            </div>
            <div style="display:flex;justify-content:space-between;">
              <span style="color:#8bb2d6;">Vessel & Helper:</span>
              <span id="rb-stat-gear" style="color:#6ad19a;">-</span>
            </div>
            <div style="display:flex;justify-content:space-between;">
              <span style="color:#8bb2d6;">Market Snipe Radar:</span>
              <b id="rb-stat-market" style="color:#ffb060;">Scanning...</b>
            </div>
            <div style="display:flex;justify-content:space-between;">
              <span style="color:#8bb2d6;">Combo / Intercepts:</span>
              <span id="rb-stat-combo">0 (0 saved)</span>
            </div>
          </div>

          <div style="margin-bottom:10px;">
            <div style="display:flex;justify-content:space-between;font-size:10px;color:#8bb2d6;margin-bottom:4px;">
              <span>Live Event Log</span>
              <span id="rb-log-count">0 events</span>
            </div>
            <div id="rb-log-box" style="height:105px;background:#060e1a;border:1px solid #142a44;border-radius:6px;padding:6px;overflow-y:auto;font-family:monospace;font-size:10.5px;line-height:1.45;"></div>
          </div>

          <button id="rb-btn-toggle" style="width:100%;padding:9px;background:#1f8a4c;color:white;border:none;border-radius:6px;font-weight:bold;cursor:pointer;transition:background 0.2s;font-size:12px;letter-spacing:0.5px;">START AUTO-PROGRESSION</button>
        </div>
      `;

      document.body.appendChild(widget);
      this.ui = widget;

      const header = widget.querySelector('#rb-header');
      let isDragging = false, startX, startY, origLeft, origTop;
      header.addEventListener('mousedown', (e) => {
        if (e.target.tagName === 'BUTTON') return;
        isDragging = true; startX = e.clientX; startY = e.clientY;
        const rect = widget.getBoundingClientRect();
        origLeft = rect.left; origTop = rect.top;
        widget.style.right = 'auto'; widget.style.bottom = 'auto';
        widget.style.left = origLeft + 'px'; widget.style.top = origTop + 'px';
      });
      window.addEventListener('mousemove', (e) => {
        if (!isDragging) return;
        widget.style.left = (origLeft + (e.clientX - startX)) + 'px';
        widget.style.top = (origTop + (e.clientY - startY)) + 'px';
      });
      window.addEventListener('mouseup', () => { isDragging = false; });

      widget.querySelector('#rb-btn-toggle').addEventListener('click', () => this.toggle());
      widget.querySelector('#rb-btn-export').addEventListener('click', () => this.exportLogs());

      const logBox = widget.querySelector('#rb-log-box');
      logBox.innerHTML = this.logs.slice(0, 15).map(e => this.formatLogHtml(e)).join('');
    },

    formatLogHtml(e) {
      const colors = {
        LEVEL_UP: '#00ff88',
        ZONE_CHANGE: '#00d2ff',
        UPGRADE: '#ffd84a',
        QUEST: '#ff7bf0',
        MARKET: '#ffb060',
        FORGE: '#ff7b7b',
        TAVERN: '#c38bff',
        DEFENSE: '#6fd8ff',
        SYSTEM: '#9bbcd6'
      };
      const col = colors[e.type] || '#d8eeff';
      return `<div style="margin-bottom:2px;"><span style="color:#557a9e;">${e.timeStr}</span> <b style="color:${col};">[${e.type}]</b> <span style="color:#c5ddf0;">${e.message}</span></div>`;
    },

    updateLogUI(newEntry) {
      if (!this.ui) return;
      const logBox = this.ui.querySelector('#rb-log-box');
      const countEl = this.ui.querySelector('#rb-log-count');
      if (logBox) {
        logBox.insertAdjacentHTML('afterbegin', this.formatLogHtml(newEntry));
        while (logBox.children.length > 50) logBox.lastElementChild.remove();
      }
      if (countEl) countEl.textContent = `${this.logs.length} events`;
    },

    updateUI(actionText) {
      if (!this.ui) return;
      const S = window.__S;
      const statusBadge = this.ui.querySelector('#rb-status-badge');
      const actionDisplay = this.ui.querySelector('#rb-action-display');
      const toggleBtn = this.ui.querySelector('#rb-btn-toggle');
      
      const captainEl = this.ui.querySelector('#rb-stat-captain');
      const coinsEl = this.ui.querySelector('#rb-stat-coins');
      const zoneEl = this.ui.querySelector('#rb-stat-zone');
      const gearEl = this.ui.querySelector('#rb-stat-gear');
      const marketEl = this.ui.querySelector('#rb-stat-market');
      const comboEl = this.ui.querySelector('#rb-stat-combo');

      if (this.running) {
        statusBadge.textContent = 'RUNNING';
        statusBadge.style.color = '#00ff88';
        statusBadge.style.background = 'rgba(0,255,136,0.15)';
        toggleBtn.textContent = 'PAUSE AUTO-BOT';
        toggleBtn.style.background = '#a82c2c';
      } else {
        statusBadge.textContent = 'STOPPED';
        statusBadge.style.color = '#ff5555';
        statusBadge.style.background = 'rgba(255,85,85,0.15)';
        toggleBtn.textContent = 'START AUTO-PROGRESSION';
        toggleBtn.style.background = '#1f8a4c';
      }

      if (actionText) actionDisplay.textContent = actionText;

      if (S?.me) {
        captainEl.textContent = `${S.me.name} (Lv ${S.me.level})`;
        const gained = (S.me.coins || 0) - this.stats.startCoins;
        coinsEl.textContent = `${gained >= 0 ? '+' : ''}${gained.toLocaleString()} (${(S.me.coins || 0).toLocaleString()} total)`;
        
        const zId = S.zone?.zone;
        zoneEl.textContent = zId !== undefined && S.defs?.zones[zId] ? `Zone ${zId} (${S.defs.zones[zId].name})` : 'Harbor';
        
        const boatDef = S.defs?.boats?.find(b => b.id === S.me.boat);
        const helperDef = S.defs?.helpers?.find(h => h.id === S.me.helper);
        gearEl.textContent = `${boatDef ? boatDef.name : S.me.boat} | ${helperDef ? helperDef.name : 'No crew'}`;
      }

      if (this.bestMarketDeal) {
        const d = this.bestMarketDeal;
        marketEl.textContent = `${d.wDef.name} (-${d.discountPct}% @ ${d.listing.price.toLocaleString()}c)`;
        marketEl.style.color = '#00ff88';
      } else {
        marketEl.textContent = this.savingGoal ? `Saving ${this.savingGoal.toLocaleString()}c for target` : 'Scanning market...';
        marketEl.style.color = '#8bb2d6';
      }

      comboEl.textContent = `${S?.combo || 0} (Best: ${this.stats.bestCombo} | ${this.stats.shotsIntercepted} deflected)`;
    }
  };

  const tryStart = () => {
    if (window.__S?.me && document.body) {
      if (!window.__ReefBlastBot) AutoBot.init();
      if (!AutoBot.running) AutoBot.start();
    } else {
      setTimeout(tryStart, 500);
    }
  };
  if (document.body) AutoBot.init();
  tryStart();
})();
