# win.ai.id

A calm, minimal daily-wins system with an AI coach and integrated Mandarin learning sub-app (`zh.win.ai.id`).

Designed to run as a single Go binary with no JS build step, using modern SQLite (CGO-free), `html/template` + `htmx`, and Alpine/vanilla JS.

---

## Features

- **Daily Wins & Habits**:
  - Global "daily win" streak: complete $\ge 1$ win (habits, one-offs, or zh drills) to maintain.
  - 1 free weekly skip automatically covers an occasional missed day.
  - Per-habit streaks calculated and displayed alongside recurring tasks.
  - Day rollover at **4:00 AM** local time (`Asia/Jakarta` default).
  - Schedule habits daily, on specific weekdays, or $N\times$ per week.
  - Quick-add one-off wins with instant keyboard/tap input.
  - Optional reflections/notes attached to individual wins.

- **AI Coach & Guardrails**:
  - Morning check-in proposing 1–3 small, actionable wins.
  - **SSE Streaming**: Real-time token streaming with instant feedback.
  - Uses OpenAI-compatible tool calling (`propose_wins`).
  - **The model never writes directly to the database**: proposals render as accept/edit cards.
  - Hard monthly budget cap (default $5/mo); automatically degrades to deterministic rule-based suggestions when exceeded or offline.
  - Transparent, editable memory notes: view, add, or delete facts the coach remembers about you.

- **Authentication**:
  - **Passkeys (WebAuthn)**: Biometric sign-in (Touch ID, Face ID, security keys).
  - **Magic link fallback**: One-time token sent to email.
  - Shared authentication across `win.ai.id` and `zh.win.ai.id` via root cookie domain `.win.ai.id`.

- **Building in Public**:
  - Public Dev Log & changelog at `/log` to share progress without leaking personal user data.

- **Subdomain Routing (`zh.win.ai.id`)**:
  - The same binary inspects the `Host` header to serve `win.ai.id` or `zh.win.ai.id`.
  - Placeholder ready for HSK 3–5 adaptive placement quiz and FSRS-driven micro-drills.

- **Installable PWA**:
  - Web App Manifest (`manifest.webmanifest`) and Service Worker (`sw.js`).
  - Web Push notification subscriptions (morning plan and evening nudge).
  - Tactile haptic feedback and SVG checkmark draw animations.
  - Respects dark mode and `prefers-reduced-motion`.

---

## Project Structure

```
win-ai-id/
├── cmd/
│   └── win/               # Entrypoint (main.go)
├── internal/
│   ├── coach/            # LLM tool calling, monthly spend tracking, fallback engine
│   ├── day/              # Pure domain rules: 4am boundary, schedules, streak logic
│   ├── store/            # SQLite schema (WAL mode, CGO-free), sessions, habits, wins
│   └── web/              # HTTP router (hostname dispatch), htmx fragments, PWA static assets
│       ├── static/       # app.css, app.js, sw.js, manifest.webmanifest, icon.svg
│       └── templates/    # layout, login, today, habits, zh HTML templates
├── Dockerfile            # Minimal multi-stage scratch/alpine container
├── docker-compose.yml    # App + Cloudflare Tunnel + Litestream
├── litestream.yml        # Continuous SQLite replication to R2/S3
└── .env.example          # Environment variables template
```

---

## Local Development

### 1. Prerequisites
- Go 1.23+

### 2. Run
```bash
cd win-ai-id
cp .env.example .env
go run ./cmd/win
```

By default the server starts on `http://localhost:8080`.

### 3. Sign In (Dev Mode)
1. Open `http://localhost:8080/login`.
2. Enter any email (or one configured in `ALLOWED_EMAILS`).
3. Because no external email service is hooked up yet, the one-time magic link is printed directly to the server terminal:
   ```
   level=INFO msg="magic login link" email=you@example.com url="http://localhost:8080/auth?token=..."
   ```
4. Click or open that link in your browser to sign in.

---

## Running Tests

All unit and integration tests run in-memory or against temporary SQLite databases:

```bash
go test -v ./...
```

---

## Production Deployment (docker-host + Cloudflare Tunnel)

1. **Configure Environment**:
   Copy `.env.example` to `.env` on `docker-host`:
   ```bash
   BASE_URL=https://win.ai.id
   COOKIE_DOMAIN=.win.ai.id
   ALLOWED_EMAILS=your.email@gmail.com
   LLM_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai
   LLM_API_KEY=your-gemini-or-openai-key
   LLM_MODEL=gemini-2.5-flash
   CLOUDFLARE_TUNNEL_TOKEN=your-tunnel-token
   ```

2. **Cloudflare Zero Trust Dashboard**:
   Add Public Hostnames on your tunnel:
   - `win.ai.id` $\to$ `http://win:8080`
   - `zh.win.ai.id` $\to$ `http://win:8080`

3. **Deploy via Docker Compose**:
   ```bash
   docker compose up -d --build
   ```
   No public inbound ports needed! SQLite data lives in volume `win-data` and replicates via Litestream.
