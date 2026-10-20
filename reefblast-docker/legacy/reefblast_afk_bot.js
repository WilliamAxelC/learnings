// ==UserScript==
// @name         Reefblast AFK Grind Bot
// @namespace    https://greentea.id/reefblast/
// @version      1.0.0
// @description  Automated AFK grinding bot for Reefblast: auto-aim, lead targeting, shot defense, auto-reenter, auto-skills, and quest claim.
// @author       Antigravity
// @match        https://greentea.id/reefblast/*
// @grant        none
// ==/UserScript==

(function () {
  'use strict';

  if (window.__ReefBlastBot) {
    if (typeof window.__ReefBlastBot.destroy === 'function') {
      window.__ReefBlastBot.destroy();
    } else if (typeof window.__ReefBlastBot.stop === 'function') {
      window.__ReefBlastBot.stop();
    }
    delete window.__ReefBlastBot;
  }

  const Bot = {
    running: false,
    timer: null,
    audioKeepAlive: null,
    
    config: {
      skillPriority: ['atk', 'aspd', 'regen', 'hp', 'crit', 'def'],
      leadTargeting: true,
      defendIncomingShots: true,
      autoZone: true,
      autoSkills: true,
      autoQuests: true,
      tickRateMs: 40
    },

    stats: {
      startedAt: Date.now(),
      startCoins: 0,
      startLevel: 1,
      startXp: 0,
      killsThisSession: 0,
      bestCombo: 0
    },

    ui: null,

    init() {
      console.log('%c[Reefblast Bot] Initializing...', 'color: #00d2ff; font-weight: bold;');
      this.createUI();
      this.bindHotkey();
      window.__ReefBlastBot = this;
      console.log('%c[Reefblast Bot] Ready! Press F2 or click START in the overlay.', 'color: #00ff88; font-weight: bold;');
    },

    bindHotkey() {
      this._keyHandler = (e) => {
        if (e.code === 'F2') {
          e.preventDefault();
          this.toggle();
        }
      };
      window.addEventListener('keydown', this._keyHandler);
    },

    toggle() {
      if (this.running) {
        this.stop();
      } else {
        this.start();
      }
    },

    start() {
      if (this.running) return;
      const S = window.__S;
      if (!S) {
        alert('Please wait until the game has fully loaded before starting the bot.');
        return;
      }

      this.running = true;
      this.stats.startedAt = Date.now();
      this.stats.startCoins = S.me?.coins || 0;
      this.stats.startLevel = S.me?.level || 1;
      this.stats.startXp = S.me?.xp || 0;
      this.stats.bestCombo = S.combo || 0;

      // Start loop
      this.timer = setInterval(() => this.step(), this.config.tickRateMs);

      // Start silent audio keepalive to prevent background tab throttling
      this.startKeepAlive();

      this.updateUI();
      console.log('%c[Reefblast Bot] Bot started.', 'color: #00ff88;');
    },

    stop() {
      this.running = false;
      if (this.timer) {
        clearInterval(this.timer);
        this.timer = null;
      }
      this.stopKeepAlive();
      if (window.__S) {
        window.__S.autofire = false;
      }
      this.updateUI();
      console.log('%c[Reefblast Bot] Bot stopped.', 'color: #ffaa00;');
    },

    destroy() {
      this.stop();
      if (this._keyHandler) {
        window.removeEventListener('keydown', this._keyHandler);
      }
      if (this.ui) {
        this.ui.remove();
        this.ui = null;
      }
      delete window.__ReefBlastBot;
    },

    startKeepAlive() {
      try {
        if (!this.audioKeepAlive) {
          const AudioCtx = window.AudioContext || window.webkitAudioContext;
          if (AudioCtx) {
            const ctx = new AudioCtx();
            const osc = ctx.createOscillator();
            const gain = ctx.createGain();
            gain.gain.value = 0.00001; // Silent
            osc.connect(gain);
            gain.connect(ctx.destination);
            osc.start();
            this.audioKeepAlive = { ctx, osc };
          }
        }
      } catch (err) {
        // audio keepalive optional
      }
    },

    stopKeepAlive() {
      if (this.audioKeepAlive) {
        try {
          this.audioKeepAlive.osc.stop();
          this.audioKeepAlive.ctx.close();
        } catch (e) {}
        this.audioKeepAlive = null;
      }
    },

    getBestZone() {
      const S = window.__S;
      if (!S || !S.defs || !S.defs.zones) return 0;
      const level = S.me?.level || 1;
      const unlocked = S.defs.zones.filter(z => level >= z.level);
      return unlocked.length ? unlocked[unlocked.length - 1].id : 0;
    },

    step() {
      const S = window.__S;
      const world = window.__world;
      if (!S || !world || !this.running) return;

      // Track combo
      if (S.combo > this.stats.bestCombo) {
        this.stats.bestCombo = S.combo;
      }

      // 1. Dismiss modals (Zone Clear, Summary, Level dialogs)
      const modal = document.getElementById('modal');
      if (modal && !modal.classList.contains('hidden')) {
        const stayBtn = modal.querySelector('[data-act="zcStay"]');
        const closeBtn = modal.querySelector('[data-close]');
        const contBtn = modal.querySelector('button.brass');
        if (stayBtn) stayBtn.click();
        else if (closeBtn) closeBtn.click();
        else if (contBtn) contBtn.click();
      }

      const reveal = document.getElementById('reveal');
      if (reveal && !reveal.classList.contains('hidden')) {
        reveal.classList.add('hidden');
      }

      // 2. Zone management: if in harbor, auto-enter best unlocked zone
      if (!S.zone && this.config.autoZone) {
        const bestZoneId = this.getBestZone();
        window.__send({ t: 'enter', zone: bestZoneId });
        this.updateUI('Entering Zone ' + bestZoneId + '...');
        return;
      }

      if (!S.zone) {
        this.updateUI('Waiting in Harbor');
        return;
      }

      // 3. Ensure autofire is active
      if (!S.autofire) {
        S.autofire = true;
      }

      // 4. Auto-spend skill points
      if (this.config.autoSkills && S.me?.points > 0) {
        const maxPerSkill = S.defs?.skillMax || 30;
        for (const skillId of this.config.skillPriority) {
          const current = (S.me.skills && S.me.skills[skillId]) || 0;
          if (current < maxPerSkill) {
            window.__send({ t: 'skill', id: skillId, n: 1 });
            break;
          }
        }
      }

      // 5. Auto-claim daily quests
      if (this.config.autoQuests && S.me?.daily?.list) {
        const ready = S.me.daily.list.filter(q => q.prog >= q.target && !q.claimed).length;
        if (ready > 0) {
          window.__send({ t: 'claim' });
        }
      }

      // 6. Defensive Target: incoming enemy shots/orbs
      if (this.config.defendIncomingShots && world.shots && world.shots.size > 0) {
        const dangerous = Array.from(world.shots.values()).filter(s => s.z > 0.5);
        if (dangerous.length > 0) {
          dangerous.sort((a, b) => b.z - a.z); // nearest to boat
          S.aim = { x: dangerous[0].x, z: dangerous[0].z };
          this.updateUI(`Defending: intercepting shot (${dangerous.length} incoming)`);
          return;
        }
      }

      // 7. Offensive Target: fish selection with velocity leading
      if (!world.fishes || world.fishes.size === 0) {
        this.updateUI('Zone active: waiting for fish spawns');
        return;
      }

      // Filter out dying, gone, warded (flag 8) and shell/thorns (flag 1)
      const validFishes = Array.from(world.fishes.values()).filter(f => {
        if (f.dying || f.gone) return false;
        if (f.flags & 8) return false; // Warded (immune)
        if (f.flags & 1) return false; // Shell (reflects damage)
        return true;
      });

      if (!validFishes.length) {
        this.updateUI('Scanning for vulnerable targets...');
        return;
      }

      // Prioritize: Bosses > lowest HP to secure quick kills and maintain combo
      validFishes.sort((a, b) => {
        const aBoss = (a.d?.boss || a.d?.mini) ? 1 : 0;
        const bBoss = (b.d?.boss || b.d?.mini) ? 1 : 0;
        if (aBoss !== bBoss) return bBoss - aBoss;
        return a.hp - b.hp;
      });

      const target = validFishes[0];
      const origin = world.origin ? world.origin(S.slot) : { x: 0, z: 7.3 };
      
      let aimX = target.x;
      let aimZ = target.z;

      if (this.config.leadTargeting) {
        const wepIdx = S.me ? S.defs.weapons.findIndex(w => w.id === S.me.equipped) : 0;
        const bulletSpeed = S.defs.weapons[Math.max(0, wepIdx)]?.speed || 25;
        const dist = Math.hypot(target.x - origin.x, target.z - origin.z);
        const flightTime = dist / bulletSpeed;
        aimX += (target.vx || 0) * flightTime;
        aimZ += (target.vz || 0) * flightTime;
      }

      // Clamp to valid field boundaries
      aimX = Math.max(-16.5, Math.min(16.5, aimX));
      aimZ = Math.max(-10.5, Math.min(4.0, aimZ));

      S.aim = { x: aimX, z: aimZ };
      const fishName = target.d?.title || target.d?.name || 'Fish';
      this.updateUI(`Targeting: ${fishName} (HP: ${Math.round(target.hp / 10)}%)`);
    },

    createUI() {
      if (document.getElementById('rb-afk-widget')) return;

      const widget = document.createElement('div');
      widget.id = 'rb-afk-widget';
      widget.style.cssText = `
        position: fixed;
        bottom: 80px;
        right: 20px;
        width: 250px;
        background: rgba(12, 22, 38, 0.92);
        border: 1px solid #1f6aa0;
        border-radius: 10px;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.6);
        color: #d8eeff;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
        font-size: 12px;
        z-index: 99999;
        backdrop-filter: blur(8px);
        user-select: none;
        overflow: hidden;
      `;

      widget.innerHTML = `
        <div id="rb-afk-header" style="background: linear-gradient(90deg, #10345a, #1a4f82); padding: 8px 12px; font-weight: bold; display: flex; justify-content: space-between; align-items: center; cursor: move; border-bottom: 1px solid #23588f;">
          <span>⚡ Reefblast AFK Bot</span>
          <span style="font-size: 10px; color: #7cb8e8; background: #0c2038; padding: 2px 6px; border-radius: 4px;">F2 Toggle</span>
        </div>
        <div style="padding: 10px 12px;">
          <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
            <span>Status:</span>
            <b id="rb-status-text" style="color: #ff5555;">STOPPED</b>
          </div>
          <div id="rb-action-text" style="font-size: 11px; color: #9bbcd6; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-bottom: 10px;">Idle</div>
          <div style="background: rgba(0,0,0,0.3); border-radius: 6px; padding: 6px 8px; margin-bottom: 10px; font-size: 11px; line-height: 1.6;">
            <div style="display: flex; justify-content: space-between;">
              <span>Coins Gained:</span>
              <b id="rb-coins-gained" style="color: #ffd84a;">+0</b>
            </div>
            <div style="display: flex; justify-content: space-between;">
              <span>Level / XP:</span>
              <span id="rb-lvl-xp">Lv 1 (0 XP)</span>
            </div>
            <div style="display: flex; justify-content: space-between;">
              <span>Best Combo:</span>
              <span id="rb-combo" style="color: #6ad19a;">0</span>
            </div>
          </div>
          <button id="rb-toggle-btn" style="width: 100%; padding: 8px; background: #1f8a4c; color: white; border: none; border-radius: 6px; font-weight: bold; cursor: pointer; transition: background 0.2s;">
            START AFK GRIND
          </button>
        </div>
      `;

      document.body.appendChild(widget);
      this.ui = widget;

      // Make draggable
      const header = widget.querySelector('#rb-afk-header');
      let isDragging = false, startX, startY, origLeft, origTop;
      header.addEventListener('mousedown', (e) => {
        isDragging = true;
        startX = e.clientX;
        startY = e.clientY;
        const rect = widget.getBoundingClientRect();
        origLeft = rect.left;
        origTop = rect.top;
        widget.style.right = 'auto';
        widget.style.bottom = 'auto';
        widget.style.left = `${origLeft}px`;
        widget.style.top = `${origTop}px`;
      });
      window.addEventListener('mousemove', (e) => {
        if (!isDragging) return;
        widget.style.left = `${origLeft + (e.clientX - startX)}px`;
        widget.style.top = `${origTop + (e.clientY - startY)}px`;
      });
      window.addEventListener('mouseup', () => { isDragging = false; });

      // Button listener
      widget.querySelector('#rb-toggle-btn').addEventListener('click', () => {
        this.toggle();
      });
    },

    updateUI(actionText) {
      if (!this.ui) return;
      const S = window.__S;
      const statusEl = this.ui.querySelector('#rb-status-text');
      const actionEl = this.ui.querySelector('#rb-action-text');
      const btn = this.ui.querySelector('#rb-toggle-btn');
      const coinsEl = this.ui.querySelector('#rb-coins-gained');
      const lvlEl = this.ui.querySelector('#rb-lvl-xp');
      const comboEl = this.ui.querySelector('#rb-combo');

      if (this.running) {
        statusEl.textContent = 'RUNNING';
        statusEl.style.color = '#00ff88';
        btn.textContent = 'STOP BOT';
        btn.style.background = '#a82c2c';
      } else {
        statusEl.textContent = 'STOPPED';
        statusEl.style.color = '#ff5555';
        btn.textContent = 'START AFK GRIND';
        btn.style.background = '#1f8a4c';
      }

      if (actionText) {
        actionEl.textContent = actionText;
      }

      if (S?.me) {
        const gainedCoins = (S.me.coins || 0) - this.stats.startCoins;
        coinsEl.textContent = (gainedCoins >= 0 ? '+' : '') + gainedCoins.toLocaleString();
        lvlEl.textContent = `Lv ${S.me.level} (${(S.me.xp || 0).toLocaleString()} XP)`;
      }

      comboEl.textContent = `${S?.combo || 0} (Best: ${this.stats.bestCombo})`;
    }
  };

  Bot.init();
})();
