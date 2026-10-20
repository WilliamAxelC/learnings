package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"winaiid/internal/coach"
	"winaiid/internal/day"
	"winaiid/internal/store"
)

func setupTestServer(t *testing.T) (*Server, *store.Store, store.User) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	u, err := st.UpsertUser(context.Background(), "tester@win.ai.id")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	co := coach.New(coach.Config{}, st)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := New(Config{
		BaseURL:       "http://localhost:8080",
		AllowedEmails: []string{"tester@win.ai.id"},
	}, st, co, logger)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return srv, st, u
}

func TestUnauthenticatedRedirect(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/habits", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 See Other, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/login") {
		t.Fatalf("expected redirect to /login, got %s", loc)
	}
}

func TestLandingPageLoggedOut(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for logged-out /, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<h1") {
		t.Fatalf("expected body to contain an <h1> tag, got: %s", body)
	}
	if !strings.Contains(body, "/login") {
		t.Fatalf("expected body to contain a link to /login, got: %s", body)
	}
	if strings.Contains(rec.Header().Get("Location"), "/login") {
		t.Fatalf("expected 200 response, not a redirect")
	}
}

func TestLoggedInHomeShowsToday(t *testing.T) {
	srv, st, u := setupTestServer(t)

	sessTok, err := st.CreateSession(context.Background(), u.ID, 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	cookie := &http.Cookie{Name: "win_session", Value: sessTok}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for logged-in home, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "win.ai.id") {
		t.Fatalf("expected today page to render, got: %s", body)
	}
	if strings.Contains(body, "Sample Day Preview") {
		t.Fatalf("expected today page, not landing page")
	}
}

func TestRobotsTxt(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /robots.txt, got %d", rec.Code)
	}
	ctype := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ctype, "text/plain") {
		t.Fatalf("expected Content-Type text/plain, got %s", ctype)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "User-agent: *") || !strings.Contains(body, "Allow: /") {
		t.Fatalf("expected robots.txt content, got: %s", body)
	}
}

func TestZhLoggedOutGuestTeaser(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	reqZh := httptest.NewRequest(http.MethodGet, "https://zh.win.ai.id/", nil)
	reqZh.Host = "zh.win.ai.id"
	recZh := httptest.NewRecorder()
	srv.ServeHTTP(recZh, reqZh)

	if recZh.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for zh guest teaser, got %d", recZh.Code)
	}
	if !strings.Contains(recZh.Body.String(), "每天一点点") {
		t.Fatalf("expected zh guest teaser to render, got: %s", recZh.Body.String())
	}
}

func TestMagicLinkLoginAndSession(t *testing.T) {
	srv, st, u := setupTestServer(t)

	// 1. Submit login form
	form := url.Values{"email": {u.Email}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login submit expected 303, got %d", rec.Code)
	}

	// 2. Generate magic link directly to authenticate
	tok, err := st.CreateMagicLink(context.Background(), u.Email, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to create magic link: %v", err)
	}

	reqAuth := httptest.NewRequest(http.MethodGet, "/auth?token="+tok, nil)
	recAuth := httptest.NewRecorder()
	srv.ServeHTTP(recAuth, reqAuth)

	if recAuth.Code != http.StatusSeeOther {
		t.Fatalf("auth callback expected 303, got %d", recAuth.Code)
	}

	cookies := recAuth.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "win_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}

	// 3. Access / with session cookie
	reqHome := httptest.NewRequest(http.MethodGet, "/", nil)
	reqHome.AddCookie(sessionCookie)
	recHome := httptest.NewRecorder()
	srv.ServeHTTP(recHome, reqHome)

	if recHome.Code != http.StatusOK {
		t.Fatalf("expected 200 OK accessing home, got %d", recHome.Code)
	}
	body := recHome.Body.String()
	if !strings.Contains(body, "win.ai.id") {
		t.Fatalf("expected home page to render, got %s", body)
	}
}

func TestAddAndToggleWin(t *testing.T) {
	srv, st, u := setupTestServer(t)

	sessTok, err := st.CreateSession(context.Background(), u.ID, 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	cookie := &http.Cookie{Name: "win_session", Value: sessTok}

	// Add win
	form := url.Values{"title": {"Drink water"}, "done": {"0"}}
	req := httptest.NewRequest(http.MethodPost, "/wins", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK after adding win, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Drink water") {
		t.Fatalf("expected win title in response fragment, got: %s", rec.Body.String())
	}

	// Check DB
	today := day.Today(time.Now(), u.Location())
	wins, err := st.ListWins(context.Background(), u.ID, today)
	if err != nil || len(wins) == 0 {
		t.Fatalf("expected wins in DB, err: %v, count: %d", err, len(wins))
	}
	targetWin := wins[0]
	if targetWin.Done {
		t.Fatal("expected new win to not be done initially")
	}

	// Toggle win
	reqToggle := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/wins/%d/toggle", targetWin.ID), nil)
	reqToggle.AddCookie(cookie)
	recToggle := httptest.NewRecorder()
	srv.ServeHTTP(recToggle, reqToggle)

	if recToggle.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on toggle, got %d", recToggle.Code)
	}

	// Verify it's toggled
	winsAfter, err := st.ListWins(context.Background(), u.ID, today)
	if err != nil || len(winsAfter) == 0 {
		t.Fatalf("failed to query wins after toggle: %v", err)
	}
	if !winsAfter[0].Done {
		t.Fatal("expected win to be marked as done after toggle")
	}
}

func TestSubdomainRouting(t *testing.T) {
	srv, st, u := setupTestServer(t)

	sessTok, err := st.CreateSession(context.Background(), u.ID, 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	cookie := &http.Cookie{Name: "win_session", Value: sessTok}

	// Request with zh.win.ai.id Host header
	reqZh := httptest.NewRequest(http.MethodGet, "https://zh.win.ai.id/", nil)
	reqZh.Host = "zh.win.ai.id"
	reqZh.AddCookie(cookie)
	recZh := httptest.NewRecorder()
	srv.ServeHTTP(recZh, reqZh)

	if recZh.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for zh subdomain, got %d", recZh.Code)
	}
	if !strings.Contains(recZh.Body.String(), "每天一点点") {
		t.Fatalf("expected zh page to render, got: %s", recZh.Body.String())
	}
}

func TestDevLogPage(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/log", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /log, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Dev Log") {
		t.Fatalf("expected dev log text in response, got: %s", rec.Body.String())
	}
}

func TestWinNote(t *testing.T) {
	srv, st, u := setupTestServer(t)

	sessTok, _ := st.CreateSession(context.Background(), u.ID, 24*time.Hour)
	cookie := &http.Cookie{Name: "win_session", Value: sessTok}

	today := day.Today(time.Now(), u.Location())
	winID, _ := st.AddWin(context.Background(), u.ID, today, "Stretch", "manual", false)

	form := url.Values{"note": {"Leg felt tight today"}}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/wins/%d/note", winID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK updating note, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Leg felt tight today") {
		t.Fatalf("expected note to render in response fragment, got: %s", rec.Body.String())
	}
}

func TestMemoryNotes(t *testing.T) {
	srv, st, u := setupTestServer(t)

	sessTok, _ := st.CreateSession(context.Background(), u.ID, 24*time.Hour)
	cookie := &http.Cookie{Name: "win_session", Value: sessTok}

	// Add memory note
	form := url.Values{"content": {"Skips gym on heavy meeting days"}}
	req := httptest.NewRequest(http.MethodPost, "/memory", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 See Other redirecting to /habits, got %d", rec.Code)
	}

	notes, err := st.ListMemoryNotes(context.Background(), u.ID)
	if err != nil || len(notes) != 1 {
		t.Fatalf("expected 1 memory note, got %v", notes)
	}

	// Delete memory note
	reqDel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/memory/%d/delete", notes[0].ID), nil)
	reqDel.AddCookie(cookie)
	recDel := httptest.NewRecorder()
	srv.ServeHTTP(recDel, reqDel)

	if recDel.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 on delete, got %d", recDel.Code)
	}

	notesAfter, _ := st.ListMemoryNotes(context.Background(), u.ID)
	if len(notesAfter) != 0 {
		t.Fatalf("expected 0 memory notes after delete, got %d", len(notesAfter))
	}
}

func TestCoachStream(t *testing.T) {
	srv, st, u := setupTestServer(t)

	sessTok, _ := st.CreateSession(context.Background(), u.ID, 24*time.Hour)
	cookie := &http.Cookie{Name: "win_session", Value: sessTok}

	form := url.Values{"message": {"Suggest a fast morning win"}}
	req := httptest.NewRequest(http.MethodPost, "/coach/stream", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on coach stream, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected text/event-stream Content-Type, got %s", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: token") || !strings.Contains(body, "event: done") {
		t.Fatalf("expected SSE events token and done, got: %s", body)
	}
}

func TestPushSubscribe(t *testing.T) {
	srv, st, u := setupTestServer(t)

	sessTok, _ := st.CreateSession(context.Background(), u.ID, 24*time.Hour)
	cookie := &http.Cookie{Name: "win_session", Value: sessTok}

	payload := `{"endpoint":"https://push.example.com/sub/123","keys":{"p256dh":"key123","auth":"auth123"}}`
	req := httptest.NewRequest(http.MethodPost, "/push/subscribe", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK subscribing to push, got %d", rec.Code)
	}

	subs, err := st.ListPushSubscriptions(context.Background(), u.ID)
	if err != nil || len(subs) != 1 || subs[0].Endpoint != "https://push.example.com/sub/123" {
		t.Fatalf("push subscription not stored correctly: %v", subs)
	}
}

func TestLoginPage(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Sign In") || !strings.Contains(body, "Create Account") || !strings.Contains(body, "name=\"password\"") {
		t.Fatalf("login page missing password auth elements: %s", body)
	}
}

func TestPasswordRegisterAndLogin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	co := coach.New(coach.Config{}, st)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := New(Config{
		BaseURL:       "http://localhost:8080",
		AllowedEmails: nil, // open registration
	}, st, co, logger)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// 1. Register with short password fails
	form := url.Values{"action": {"register"}, "email": {"newuser@win.ai.id"}, "password": {"short"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("expected redirect with error for short password, got code %d, loc %s", rec.Code, rec.Header().Get("Location"))
	}

	// 2. Register new user with valid password
	form = url.Values{"action": {"register"}, "email": {"newuser@win.ai.id"}, "password": {"secretpassword123"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("expected 303 redirect to /, got code %d, loc %s", rec.Code, rec.Header().Get("Location"))
	}
	// Verify session cookie was set
	cookies := rec.Result().Cookies()
	var sessCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "win_session" {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil || sessCookie.Value == "" {
		t.Fatal("expected win_session cookie to be set after registration")
	}

	// 3. Login with wrong password fails
	form = url.Values{"action": {"login"}, "email": {"newuser@win.ai.id"}, "password": {"wrongpassword"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("expected redirect with error for wrong password, got code %d, loc %s", rec.Code, rec.Header().Get("Location"))
	}

	// 4. Login with correct password succeeds
	form = url.Values{"action": {"login"}, "email": {"newuser@win.ai.id"}, "password": {"secretpassword123"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("expected 303 redirect to / on login, got code %d, loc %s", rec.Code, rec.Header().Get("Location"))
	}
}
