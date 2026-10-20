package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"winaiid/internal/coach"
	"winaiid/internal/day"
	"winaiid/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const (
	sessionCookie = "win_session"
	sessionTTL    = 90 * 24 * time.Hour
	magicTTL      = 15 * time.Minute
)

// Config holds server settings.
type Config struct {
	BaseURL       string   // public URL of the core app, e.g. https://win.ai.id
	CookieDomain  string   // ".win.ai.id" in prod so zh. shares login; "" for localhost
	SecureCookies bool     // true behind HTTPS
	AllowedEmails []string // empty = anyone may sign in
}

// Server is the HTTP app.
type Server struct {
	cfg       Config
	store     *store.Store
	coach     *coach.Coach
	log       *slog.Logger
	pages     map[string]*template.Template
	core      *http.ServeMux
	zh        *http.ServeMux
	now       func() time.Time
	webauthn  *webauthn.WebAuthn
	waSession sync.Map // sessionID -> *webauthn.SessionData
}

// New wires routes and templates.
func New(cfg Config, st *store.Store, co *coach.Coach, log *slog.Logger) (*Server, error) {
	s := &Server{cfg: cfg, store: st, coach: co, log: log, now: time.Now, pages: map[string]*template.Template{}}
	funcs := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}
	for _, p := range []string{"login", "today", "habits", "zh", "log", "landing"} {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+p+".html")
		if err != nil {
			return nil, err
		}
		s.pages[p] = t
	}

	rpID := "localhost"
	if cfg.CookieDomain != "" {
		rpID = strings.TrimPrefix(cfg.CookieDomain, ".")
	}
	origins := []string{cfg.BaseURL}
	if strings.HasPrefix(cfg.BaseURL, "http://localhost") {
		origins = append(origins, "http://localhost:8080", "http://127.0.0.1:8080")
	} else {
		origins = append(origins, "https://win.ai.id", "https://zh.win.ai.id")
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "win.ai.id",
		RPID:          rpID,
		RPOrigins:     origins,
	})
	if err != nil {
		return nil, fmt.Errorf("init webauthn: %w", err)
	}
	s.webauthn = wa

	static, _ := fs.Sub(staticFS, "static")
	files := http.FileServer(http.FS(static))

	core := http.NewServeMux()
	core.Handle("GET /static/", http.StripPrefix("/static/", files))
	core.HandleFunc("GET /manifest.webmanifest", serveStatic(static, "manifest.webmanifest", "application/manifest+json"))
	core.HandleFunc("GET /sw.js", serveStatic(static, "sw.js", "text/javascript")) // root scope
	core.HandleFunc("GET /robots.txt", serveStatic(static, "robots.txt", "text/plain"))
	core.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	core.HandleFunc("GET /log", s.logPage)
	core.HandleFunc("GET /login", s.loginPage)
	core.HandleFunc("POST /login", s.loginSubmit)
	core.HandleFunc("GET /auth", s.authCallback)
	core.HandleFunc("POST /logout", s.logout)
	core.HandleFunc("GET /auth/passkey/login-begin", s.passkeyLoginBegin)
	core.HandleFunc("POST /auth/passkey/login-finish", s.passkeyLoginFinish)
	core.HandleFunc("GET /auth/passkey/register-begin", s.authed(s.passkeyRegisterBegin))
	core.HandleFunc("POST /auth/passkey/register-finish", s.authed(s.passkeyRegisterFinish))
	core.HandleFunc("GET /{$}", s.rootPage)
	core.HandleFunc("POST /wins", s.authed(s.addWin))
	core.HandleFunc("POST /wins/{id}/toggle", s.authed(s.toggleWin))
	core.HandleFunc("POST /wins/{id}/delete", s.authed(s.deleteWin))
	core.HandleFunc("POST /wins/{id}/note", s.authed(s.updateWinNote))
	core.HandleFunc("POST /coach", s.authed(s.coachTurn))
	core.HandleFunc("POST /coach/stream", s.authed(s.coachStream))
	core.HandleFunc("POST /proposals/{id}/accept", s.authed(s.decideProposal(true)))
	core.HandleFunc("POST /proposals/{id}/dismiss", s.authed(s.decideProposal(false)))
	core.HandleFunc("GET /habits", s.authed(s.habitsPage))
	core.HandleFunc("POST /habits", s.authed(s.createHabit))
	core.HandleFunc("POST /habits/{id}/archive", s.authed(s.archiveHabit))
	core.HandleFunc("POST /about", s.authed(s.saveAbout))
	core.HandleFunc("POST /memory", s.authed(s.addMemoryNote))
	core.HandleFunc("POST /memory/{id}/delete", s.authed(s.deleteMemoryNote))
	core.HandleFunc("POST /push/subscribe", s.authed(s.pushSubscribe))
	s.core = core

	zh := http.NewServeMux()
	zh.Handle("GET /static/", http.StripPrefix("/static/", files))
	zh.HandleFunc("GET /robots.txt", serveStatic(static, "robots.txt", "text/plain"))
	zh.HandleFunc("GET /log", s.logPage)
	zh.HandleFunc("GET /{$}", s.zhPage)
	s.zh = zh
	return s, nil
}

// ServeHTTP dispatches on the zh. subdomain.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(strings.ToLower(r.Host), "zh.") {
		s.zh.ServeHTTP(w, r)
		return
	}
	s.core.ServeHTTP(w, r)
}

func serveStatic(fsys fs.FS, name, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	}
}

// ---- auth ----------------------------------------------------------------

type ctxKey struct{}

func userFrom(r *http.Request) store.User { return r.Context().Value(ctxKey{}).(store.User) }

func (s *Server) userFromSession(r *http.Request) (store.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, false
	}
	u, err := s.store.UserBySession(r.Context(), c.Value)
	if err != nil {
		return store.User{}, false
	}
	return u, true
}

func (s *Server) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u, ok := s.userFromSession(r); ok {
			h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", s.cfg.BaseURL+"/login")
			return
		}
		http.Redirect(w, r, s.cfg.BaseURL+"/login", http.StatusSeeOther)
	}
}

func (s *Server) rootPage(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.userFromSession(r); ok {
		s.todayPage(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
		return
	}
	s.landingPage(w, r)
}

func (s *Server) landingPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "landing", map[string]any{"BaseURL": s.cfg.BaseURL})
}

func (s *Server) emailAllowed(email string) bool {
	if len(s.cfg.AllowedEmails) == 0 {
		return true
	}
	for _, a := range s.cfg.AllowedEmails {
		if strings.EqualFold(strings.TrimSpace(a), email) {
			return true
		}
	}
	return false
}

func (s *Server) setSessionCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		Expires:  s.now().Add(sessionTTL),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab != "register" {
		tab = "login"
	}
	s.render(w, "login", map[string]any{
		"Error": r.URL.Query().Get("error"),
		"Email": r.URL.Query().Get("email"),
		"Tab":   tab,
	})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	action := r.FormValue("action")
	if action != "register" {
		action = "login"
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")

	if email == "" || !strings.Contains(email, "@") {
		http.Redirect(w, r, "/login?error=Please+enter+a+valid+email+address&tab="+action+"&email="+url.QueryEscape(email), http.StatusSeeOther)
		return
	}

	if len(password) < 8 {
		http.Redirect(w, r, "/login?error=Password+must+be+at+least+8+characters&tab="+action+"&email="+url.QueryEscape(email), http.StatusSeeOther)
		return
	}

	if action == "register" {
		if !s.emailAllowed(email) {
			http.Redirect(w, r, "/login?error=Registration+is+restricted+to+authorized+users&tab=register", http.StatusSeeOther)
			return
		}
		u, err := s.store.CreateUserWithPassword(r.Context(), email, password)
		if errors.Is(err, store.ErrEmailTaken) {
			http.Redirect(w, r, "/login?error=An+account+with+this+email+already+exists.+Please+sign+in.&tab=login&email="+url.QueryEscape(email), http.StatusSeeOther)
			return
		} else if err != nil {
			s.fail(w, err)
			return
		}
		tok, err := s.store.CreateSession(r.Context(), u.ID, sessionTTL)
		if err != nil {
			s.fail(w, err)
			return
		}
		s.setSessionCookie(w, tok)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Login
	u, err := s.store.AuthenticateUser(r.Context(), email, password)
	if errors.Is(err, store.ErrInvalidCredentials) {
		http.Redirect(w, r, "/login?error=Invalid+email+or+password&tab=login&email="+url.QueryEscape(email), http.StatusSeeOther)
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}

	tok, err := s.store.CreateSession(r.Context(), u.ID, sessionTTL)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.setSessionCookie(w, tok)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	email, err := s.store.ConsumeMagicLink(r.Context(), r.URL.Query().Get("token"))
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "This login link is invalid or expired.", http.StatusUnauthorized)
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	u, err := s.store.UpsertUser(r.Context(), email)
	if err != nil {
		s.fail(w, err)
		return
	}
	tok, err := s.store.CreateSession(r.Context(), u.ID, sessionTTL)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.setSessionCookie(w, tok)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", Domain: s.cfg.CookieDomain, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- today ---------------------------------------------------------------

type todayView struct {
	User         store.User
	Today        string
	Pretty       string
	Wins         []store.Win
	Done         int
	Streak       day.StreakResult
	HabitStreaks map[int64]int
	Proposals    []store.Proposal
	Messages     []store.Message
	CoachEnabled bool
}

func (s *Server) today(u store.User) string { return day.Today(s.now(), u.Location()) }

func (s *Server) loadToday(ctx context.Context, u store.User) (todayView, error) {
	today := s.today(u)
	v := todayView{User: u, Today: today, CoachEnabled: s.coach.Enabled()}
	if t, err := day.Parse(today); err == nil {
		v.Pretty = t.Format("Monday, 2 January")
	}
	if err := s.store.EnsureHabitWins(ctx, u.ID, today); err != nil {
		return v, err
	}
	var err error
	if v.Wins, err = s.store.ListWins(ctx, u.ID, today); err != nil {
		return v, err
	}
	for _, w := range v.Wins {
		if w.Done {
			v.Done++
		}
	}
	won, err := s.store.WonDays(ctx, u.ID, day.Add(today, -400))
	if err != nil {
		return v, err
	}
	v.Streak = day.Streak(today, func(d string) bool { return won[d] })

	v.HabitStreaks = map[int64]int{}
	habits, err := s.store.ListHabits(ctx, u.ID)
	if err == nil {
		allHabitDays, _ := s.store.AllHabitsWonDays(ctx, u.ID, day.Add(today, -365))
		for _, h := range habits {
			daysMap := allHabitDays[h.ID]
			v.HabitStreaks[h.ID] = day.HabitStreak(today, h.Schedule, func(d string) bool { return daysMap != nil && daysMap[d] })
		}
	}

	if v.Proposals, err = s.store.PendingProposals(ctx, u.ID, today); err != nil {
		return v, err
	}
	v.Messages, err = s.store.ListMessages(ctx, u.ID, today)
	return v, err
}

func (s *Server) todayPage(w http.ResponseWriter, r *http.Request) {
	v, err := s.loadToday(r.Context(), userFrom(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "today", v)
}

// renderApp re-renders the #app fragment after any mutation.
func (s *Server) renderApp(w http.ResponseWriter, r *http.Request) {
	v, err := s.loadToday(r.Context(), userFrom(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.renderBlock(w, "today", "app", v)
}

func (s *Server) addWin(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	title := strings.TrimSpace(r.FormValue("title"))
	if title != "" {
		if len([]rune(title)) > 140 {
			title = string([]rune(title)[:140])
		}
		if _, err := s.store.AddWin(r.Context(), u.ID, s.today(u), title, "manual", r.FormValue("done") == "1"); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.renderApp(w, r)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

func (s *Server) toggleWin(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if _, err := s.store.ToggleWin(r.Context(), userFrom(r).ID, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, err)
		return
	}
	s.renderApp(w, r)
}

func (s *Server) deleteWin(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteWin(r.Context(), userFrom(r).ID, id); err != nil {
		s.fail(w, err)
		return
	}
	s.renderApp(w, r)
}

// ---- coach ---------------------------------------------------------------

func (s *Server) coachTurn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	today := s.today(u)
	msg := strings.TrimSpace(r.FormValue("message"))
	if len([]rune(msg)) > 1000 {
		msg = string([]rune(msg)[:1000])
	}

	in := coach.Input{Today: today, About: u.About, UserMessage: msg}
	if t, err := day.Parse(today); err == nil {
		in.Weekday = t.Weekday().String()
	}
	var err error
	if in.Habits, err = s.store.ListHabits(ctx, u.ID); err != nil {
		s.fail(w, err)
		return
	}
	if in.History, err = s.store.WinsBetween(ctx, u.ID, day.Add(today, -14), day.Add(today, -1)); err != nil {
		s.fail(w, err)
		return
	}
	if in.TodayWins, err = s.store.ListWins(ctx, u.ID, today); err != nil {
		s.fail(w, err)
		return
	}
	if in.Memory, err = s.store.MemoryNotes(ctx, u.ID); err != nil {
		s.fail(w, err)
		return
	}
	if in.Chat, err = s.store.ListMessages(ctx, u.ID, today); err != nil {
		s.fail(w, err)
		return
	}

	out := s.coach.Plan(ctx, in)
	shown := msg
	if shown == "" {
		shown = "Plan my day."
	}
	if err := s.store.AddMessage(ctx, u.ID, today, "user", shown); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.AddMessage(ctx, u.ID, today, "assistant", out.Reply); err != nil {
		s.fail(w, err)
		return
	}
	for _, p := range out.Proposals {
		if err := s.store.AddProposal(ctx, u.ID, today, p.Title, p.Reason); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.renderApp(w, r)
}

func (s *Server) decideProposal(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		err := s.store.DecideProposal(r.Context(), userFrom(r).ID, id, accept, r.FormValue("title"))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.fail(w, err)
			return
		}
		s.renderApp(w, r)
	}
}

func (s *Server) saveAbout(w http.ResponseWriter, r *http.Request) {
	about := strings.TrimSpace(r.FormValue("about"))
	if len([]rune(about)) > 2000 {
		about = string([]rune(about)[:2000])
	}
	if err := s.store.UpdateAbout(r.Context(), userFrom(r).ID, about); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

// ---- habits --------------------------------------------------------------

func (s *Server) habitsPage(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hs, err := s.store.ListHabits(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	memNotes, _ := s.store.ListMemoryNotes(r.Context(), u.ID)
	today := s.today(u)
	allHabitDays, _ := s.store.AllHabitsWonDays(r.Context(), u.ID, day.Add(today, -365))
	streaks := map[int64]int{}
	for _, h := range hs {
		daysMap := allHabitDays[h.ID]
		streaks[h.ID] = day.HabitStreak(today, h.Schedule, func(d string) bool { return daysMap != nil && daysMap[d] })
	}
	s.render(w, "habits", map[string]any{
		"User":         u,
		"Habits":       hs,
		"HabitStreaks": streaks,
		"MemoryNotes":  memNotes,
	})
}

func (s *Server) createHabit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	sc := day.Schedule{Kind: day.ScheduleKind(r.FormValue("kind"))}
	switch sc.Kind {
	case day.Daily:
	case day.Weekdays:
		for _, v := range r.Form["wd"] {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 6 {
				sc.WeekdayMask |= 1 << n
			}
		}
		if sc.WeekdayMask == 0 {
			sc.Kind = day.Daily
		}
	case day.PerWeek:
		n, _ := strconv.Atoi(r.FormValue("per_week"))
		if n < 1 || n > 7 {
			n = 3
		}
		sc.PerWeek = n
	default:
		sc.Kind = day.Daily
	}
	if title != "" {
		if err := s.store.CreateHabit(r.Context(), userFrom(r).ID, title, sc); err != nil {
			s.fail(w, err)
			return
		}
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (s *Server) archiveHabit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.store.ArchiveHabit(r.Context(), userFrom(r).ID, id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (s *Server) updateWinNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	note := r.FormValue("note")
	if len([]rune(note)) > 500 {
		note = string([]rune(note)[:500])
	}
	if err := s.store.UpdateWinNote(r.Context(), userFrom(r).ID, id, note); err != nil {
		s.fail(w, err)
		return
	}
	s.renderApp(w, r)
}

func (s *Server) addMemoryNote(w http.ResponseWriter, r *http.Request) {
	content := strings.TrimSpace(r.FormValue("content"))
	if content != "" {
		if len([]rune(content)) > 500 {
			content = string([]rune(content)[:500])
		}
		_ = s.store.AddMemoryNote(r.Context(), userFrom(r).ID, content)
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (s *Server) deleteMemoryNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	_ = s.store.DeleteMemoryNote(r.Context(), userFrom(r).ID, id)
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256DH string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Endpoint == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := s.store.SavePushSubscription(r.Context(), u.ID, req.Endpoint, req.Keys.P256DH, req.Keys.Auth); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"subscribed"}`))
}

func (s *Server) logPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "log", map[string]any{"BaseURL": s.cfg.BaseURL})
}

func (s *Server) coachStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	today := s.today(u)
	msg := strings.TrimSpace(r.FormValue("message"))
	if len([]rune(msg)) > 1000 {
		msg = string([]rune(msg)[:1000])
	}

	in := coach.Input{Today: today, About: u.About, UserMessage: msg}
	if t, err := day.Parse(today); err == nil {
		in.Weekday = t.Weekday().String()
	}
	in.Habits, _ = s.store.ListHabits(ctx, u.ID)
	in.History, _ = s.store.WinsBetween(ctx, u.ID, day.Add(today, -14), day.Add(today, -1))
	in.TodayWins, _ = s.store.ListWins(ctx, u.ID, today)
	in.Memory, _ = s.store.MemoryNotes(ctx, u.ID)
	in.Chat, _ = s.store.ListMessages(ctx, u.ID, today)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	out := s.coach.PlanStream(ctx, in, func(delta string) {
		payload, _ := json.Marshal(map[string]string{"delta": delta})
		fmt.Fprintf(w, "event: token\ndata: %s\n\n", payload)
		flusher.Flush()
	})

	shown := msg
	if shown == "" {
		shown = "Plan my day."
	}
	_ = s.store.AddMessage(ctx, u.ID, today, "user", shown)
	_ = s.store.AddMessage(ctx, u.ID, today, "assistant", out.Reply)
	for _, p := range out.Proposals {
		_ = s.store.AddProposal(ctx, u.ID, today, p.Title, p.Reason)
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	options, sessionData, err := s.webauthn.BeginDiscoverableLogin()
	if err != nil {
		s.log.Error("passkey begin login err", "err", err)
		http.Error(w, "failed to begin passkey login", http.StatusInternalServerError)
		return
	}
	tok := token()
	s.waSession.Store("login:"+tok, sessionData)
	http.SetCookie(w, &http.Cookie{
		Name:     "wa_challenge",
		Value:    tok,
		Path:     "/",
		Expires:  s.now().Add(5 * time.Minute),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(options)
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("wa_challenge")
	if err != nil {
		http.Error(w, "missing challenge", http.StatusBadRequest)
		return
	}
	val, ok := s.waSession.LoadAndDelete("login:" + c.Value)
	if !ok {
		http.Error(w, "expired challenge", http.StatusBadRequest)
		return
	}
	sessionData := val.(*webauthn.SessionData)

	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.store.PasskeyUserByCredentialID(r.Context(), rawID)
		if err != nil {
			return nil, err
		}
		return s.store.LoadWebAuthnUser(r.Context(), u)
	}

	cred, err := s.webauthn.FinishDiscoverableLogin(handler, *sessionData, r)
	if err != nil {
		s.log.Error("passkey finish error", "err", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	_ = s.store.UpdatePasskeySignCount(r.Context(), cred.ID, cred.Authenticator.SignCount)

	u, err := s.store.PasskeyUserByCredentialID(r.Context(), cred.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	tok, err := s.store.CreateSession(r.Context(), u.ID, sessionTTL)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.setSessionCookie(w, tok)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","redirect":"/"}`))
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	waUser, err := s.store.LoadWebAuthnUser(r.Context(), u)
	if err != nil {
		s.fail(w, err)
		return
	}
	options, sessionData, err := s.webauthn.BeginRegistration(waUser)
	if err != nil {
		s.log.Error("passkey begin reg err", "err", err)
		http.Error(w, "failed to begin passkey registration", http.StatusInternalServerError)
		return
	}
	tok := token()
	s.waSession.Store("reg:"+tok, sessionData)
	http.SetCookie(w, &http.Cookie{
		Name:     "wa_challenge",
		Value:    tok,
		Path:     "/",
		Expires:  s.now().Add(5 * time.Minute),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(options)
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	c, err := r.Cookie("wa_challenge")
	if err != nil {
		http.Error(w, "missing challenge", http.StatusBadRequest)
		return
	}
	val, ok := s.waSession.LoadAndDelete("reg:" + c.Value)
	if !ok {
		http.Error(w, "expired challenge", http.StatusBadRequest)
		return
	}
	sessionData := val.(*webauthn.SessionData)
	waUser, err := s.store.LoadWebAuthnUser(r.Context(), u)
	if err != nil {
		s.fail(w, err)
		return
	}
	cred, err := s.webauthn.FinishRegistration(waUser, *sessionData, r)
	if err != nil {
		s.log.Error("passkey registration finish error", "err", err)
		http.Error(w, "registration failed", http.StatusBadRequest)
		return
	}
	if err := s.store.SavePasskey(r.Context(), u.ID, cred); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ---- zh (placeholder until after 2–3 weeks of core usage) ---------------

func (s *Server) zhPage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFromSession(r)
	if !ok {
		u = store.User{Email: "guest"}
	}
	s.render(w, "zh", map[string]any{"User": u, "CoreURL": s.cfg.BaseURL})
}

// ---- rendering -----------------------------------------------------------

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	s.renderBlock(w, page, "layout.html", data)
}

func (s *Server) renderBlock(w http.ResponseWriter, page, block string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pages[page].ExecuteTemplate(w, block, data); err != nil {
		s.log.Error("render", "page", page, "block", block, "err", err)
	}
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.log.Error("request failed", "err", err)
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
