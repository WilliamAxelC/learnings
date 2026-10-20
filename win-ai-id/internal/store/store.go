// Package store is the SQLite persistence layer for win.ai.id.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"winaiid/internal/day"
)

//go:embed schema.sql
var schema string

// ErrNotFound is returned when a lookup matches nothing.
var ErrNotFound = errors.New("not found")

// Store wraps the database handle.
type Store struct{ db *sql.DB }

// Open opens (or creates) the SQLite database at path and applies the schema.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single writer keeps SQLite simple; plenty for this scale
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Migrate existing tables to ensure password_hash column exists
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''`)
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ---- users & auth --------------------------------------------------------

// User is an account.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	TZ           string
	About        string
}

// Location returns the user's time zone, defaulting to Asia/Jakarta.
func (u User) Location() *time.Location {
	if loc, err := time.LoadLocation(u.TZ); err == nil {
		return loc
	}
	loc, _ := time.LoadLocation("Asia/Jakarta")
	return loc
}

// WebAuthnID implements webauthn.User.
func (u User) WebAuthnID() []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(u.ID))
	return b
}

// WebAuthnName implements webauthn.User.
func (u User) WebAuthnName() string {
	return u.Email
}

// WebAuthnDisplayName implements webauthn.User.
func (u User) WebAuthnDisplayName() string {
	return u.Email
}

// WebAuthnCredentials implements webauthn.User.
func (u User) WebAuthnCredentials() []webauthn.Credential {
	return nil
}

// WebAuthnUser wraps User with loaded credentials.
type WebAuthnUser struct {
	User
	credentials []webauthn.Credential
}

func (w WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return w.credentials
}

// LoadWebAuthnUser wraps a User with its registered passkey credentials.
func (s *Store) LoadWebAuthnUser(ctx context.Context, u User) (WebAuthnUser, error) {
	creds, err := s.UserPasskeys(ctx, u.ID)
	if err != nil {
		return WebAuthnUser{}, err
	}
	return WebAuthnUser{User: u, credentials: creds}, nil
}

// UpsertUser returns the user with email, creating it if needed.
func (s *Store) UpsertUser(ctx context.Context, email string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := s.db.ExecContext(ctx, `INSERT INTO users(email) VALUES(?) ON CONFLICT(email) DO NOTHING`, email); err != nil {
		return User{}, err
	}
	return s.UserByEmail(ctx, email)
}

// UserByEmail looks up a user by email.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash,tz,about FROM users WHERE email=?`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TZ, &u.About)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// UserByID looks up a user by primary key ID.
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash,tz,about FROM users WHERE id=?`, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TZ, &u.About)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken          = errors.New("email already registered")
	ErrPasswordTooShort    = errors.New("password must be at least 8 characters")
)

// Precomputed dummy bcrypt hash for constant-time comparisons when email is not found
var dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// SetPassword updates a user's password.
func (s *Store) SetPassword(ctx context.Context, uid int64, password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, string(hash), uid)
	return err
}

// CreateUserWithPassword creates a new user account with email and password.
func (s *Store) CreateUserWithPassword(ctx context.Context, email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return User{}, errors.New("invalid email address")
	}
	if len(password) < 8 {
		return User{}, ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO users(email, password_hash) VALUES(?, ?)`, email, string(hash))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.UserByID(ctx, id)
}

// AuthenticateUser verifies email and password, returning the user if credentials match.
func (s *Store) AuthenticateUser(ctx context.Context, email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.UserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return User{}, ErrInvalidCredentials
	} else if err != nil {
		return User{}, err
	}
	if u.PasswordHash == "" {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}

// UpdateAbout saves the user's free-text "about me" given to the coach.
func (s *Store) UpdateAbout(ctx context.Context, uid int64, about string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET about=? WHERE id=?`, about, uid)
	return err
}

// CreateMagicLink stores a one-time login token for email.
func (s *Store) CreateMagicLink(ctx context.Context, email string, ttl time.Duration) (string, error) {
	t := token()
	_, err := s.db.ExecContext(ctx, `INSERT INTO magic_links(token,email,expires_at) VALUES(?,?,?)`,
		t, strings.ToLower(strings.TrimSpace(email)), ts(time.Now().Add(ttl)))
	return t, err
}

// ConsumeMagicLink validates and burns a token, returning its email.
func (s *Store) ConsumeMagicLink(ctx context.Context, t string) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx,
		`UPDATE magic_links SET used=1 WHERE token=? AND used=0 AND expires_at>? RETURNING email`,
		t, ts(time.Now())).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// CreateSession starts a session for uid.
func (s *Store) CreateSession(ctx context.Context, uid int64, ttl time.Duration) (string, error) {
	t := token()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`, t, uid, ts(time.Now().Add(ttl)))
	return t, err
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, t string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token=?`, t)
	return err
}

// UserBySession resolves a session token.
func (s *Store) UserBySession(ctx context.Context, t string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.email,u.tz,u.about FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token=? AND s.expires_at>?`, t, ts(time.Now())).Scan(&u.ID, &u.Email, &u.TZ, &u.About)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// ---- habits --------------------------------------------------------------

// Habit is a recurring win.
type Habit struct {
	ID       int64
	Title    string
	Schedule day.Schedule
}

// Describe renders the schedule for humans.
func (h Habit) Describe() string {
	switch h.Schedule.Kind {
	case day.Daily:
		return "every day"
	case day.PerWeek:
		return fmt.Sprintf("%d× per week", h.Schedule.PerWeek)
	case day.Weekdays:
		names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
		var out []string
		for i := 1; i <= 7; i++ { // Monday first
			wd := i % 7
			if h.Schedule.WeekdayMask&(1<<wd) != 0 {
				out = append(out, names[wd])
			}
		}
		return strings.Join(out, " ")
	}
	return ""
}

// ListHabits returns active habits.
func (s *Store) ListHabits(ctx context.Context, uid int64) ([]Habit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,kind,weekday_mask,per_week FROM habits
		WHERE user_id=? AND archived=0 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Habit
	for rows.Next() {
		var h Habit
		var kind string
		if err := rows.Scan(&h.ID, &h.Title, &kind, &h.Schedule.WeekdayMask, &h.Schedule.PerWeek); err != nil {
			return nil, err
		}
		h.Schedule.Kind = day.ScheduleKind(kind)
		out = append(out, h)
	}
	return out, rows.Err()
}

// CreateHabit adds a habit.
func (s *Store) CreateHabit(ctx context.Context, uid int64, title string, sc day.Schedule) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO habits(user_id,title,kind,weekday_mask,per_week) VALUES(?,?,?,?,?)`,
		uid, title, string(sc.Kind), sc.WeekdayMask, sc.PerWeek)
	return err
}

// ArchiveHabit hides a habit going forward (history is kept).
func (s *Store) ArchiveHabit(ctx context.Context, uid, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE habits SET archived=1 WHERE id=? AND user_id=?`, id, uid)
	return err
}

// ---- wins ----------------------------------------------------------------

// Win is one item on a day's list.
type Win struct {
	ID      int64
	Day     string
	Title   string
	HabitID sql.NullInt64
	Source  string
	Note    string
	Done    bool
}

// EnsureHabitWins materializes today's due habits as (undone) wins.
func (s *Store) EnsureHabitWins(ctx context.Context, uid int64, today string) error {
	habits, err := s.ListHabits(ctx, uid)
	if err != nil {
		return err
	}
	weekStart := day.WeekStart(today)
	for _, h := range habits {
		var doneBefore int
		if h.Schedule.Kind == day.PerWeek {
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wins WHERE user_id=? AND habit_id=?
				AND day>=? AND day<? AND done_at IS NOT NULL`, uid, h.ID, weekStart, today).Scan(&doneBefore); err != nil {
				return err
			}
		}
		if !h.Schedule.Due(today, doneBefore) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO wins(user_id,day,title,habit_id,source) VALUES(?,?,?,?, 'habit')
			ON CONFLICT DO NOTHING`, uid, today, h.Title, h.ID); err != nil {
			return err
		}
	}
	return nil
}

// ListWins returns a day's wins: undone first, then by creation.
func (s *Store) ListWins(ctx context.Context, uid int64, d string) ([]Win, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,day,title,habit_id,source,note,done_at IS NOT NULL FROM wins
		WHERE user_id=? AND day=? ORDER BY habit_id IS NULL, id`, uid, d)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Win
	for rows.Next() {
		var w Win
		if err := rows.Scan(&w.ID, &w.Day, &w.Title, &w.HabitID, &w.Source, &w.Note, &w.Done); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddWin creates a one-off win (optionally already done).
func (s *Store) AddWin(ctx context.Context, uid int64, d, title, source string, done bool) (int64, error) {
	var doneAt any
	if done {
		doneAt = ts(time.Now())
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO wins(user_id,day,title,source,done_at) VALUES(?,?,?,?,?)`, uid, d, title, source, doneAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ToggleWin flips done state and returns the updated win.
func (s *Store) ToggleWin(ctx context.Context, uid, id int64) (Win, error) {
	if _, err := s.db.ExecContext(ctx, `UPDATE wins SET done_at = CASE WHEN done_at IS NULL THEN ? ELSE NULL END
		WHERE id=? AND user_id=?`, ts(time.Now()), id, uid); err != nil {
		return Win{}, err
	}
	var w Win
	err := s.db.QueryRowContext(ctx, `SELECT id,day,title,habit_id,source,note,done_at IS NOT NULL FROM wins WHERE id=? AND user_id=?`,
		id, uid).Scan(&w.ID, &w.Day, &w.Title, &w.HabitID, &w.Source, &w.Note, &w.Done)
	if errors.Is(err, sql.ErrNoRows) {
		return Win{}, ErrNotFound
	}
	return w, err
}

// DeleteWin removes a one-off win (habit wins can't be deleted, only left undone).
func (s *Store) DeleteWin(ctx context.Context, uid, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wins WHERE id=? AND user_id=? AND habit_id IS NULL`, id, uid)
	return err
}

// WonDays returns the set of days since `since` with at least one done win.
func (s *Store) WonDays(ctx context.Context, uid int64, since string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT day FROM wins WHERE user_id=? AND day>=? AND done_at IS NOT NULL`, uid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out[d] = true
	}
	return out, rows.Err()
}

// WinsBetween returns all wins in [from, to] inclusive, for coach context.
func (s *Store) WinsBetween(ctx context.Context, uid int64, from, to string) ([]Win, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,day,title,habit_id,source,note,done_at IS NOT NULL FROM wins
		WHERE user_id=? AND day>=? AND day<=? ORDER BY day, id`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Win
	for rows.Next() {
		var w Win
		if err := rows.Scan(&w.ID, &w.Day, &w.Title, &w.HabitID, &w.Source, &w.Note, &w.Done); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ---- coach ---------------------------------------------------------------

// Message is one coach chat turn.
type Message struct {
	Role    string
	Content string
}

// AddMessage appends a chat turn.
func (s *Store) AddMessage(ctx context.Context, uid int64, d, role, content string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO coach_messages(user_id,day,role,content) VALUES(?,?,?,?)`, uid, d, role, content)
	return err
}

// ListMessages returns a day's chat.
func (s *Store) ListMessages(ctx context.Context, uid int64, d string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role,content FROM coach_messages WHERE user_id=? AND day=? ORDER BY id`, uid, d)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Proposal is a coach-suggested win awaiting the user's accept/dismiss.
type Proposal struct {
	ID     int64
	Title  string
	Reason string
}

// AddProposal stores a pending suggestion.
func (s *Store) AddProposal(ctx context.Context, uid int64, d, title, reason string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO proposals(user_id,day,title,reason) VALUES(?,?,?,?)`, uid, d, title, reason)
	return err
}

// PendingProposals lists a day's undecided suggestions.
func (s *Store) PendingProposals(ctx context.Context, uid int64, d string) ([]Proposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,reason FROM proposals WHERE user_id=? AND day=? AND status='pending' ORDER BY id`, uid, d)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		var p Proposal
		if err := rows.Scan(&p.ID, &p.Title, &p.Reason); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecideProposal accepts (optionally with an edited title) or dismisses a
// proposal. Accepting creates a coach-sourced win: this is the only path from
// LLM output to the wins table, and it always requires a user action.
func (s *Store) DecideProposal(ctx context.Context, uid, id int64, accept bool, editedTitle string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var d, title string
	err = tx.QueryRowContext(ctx, `SELECT day,title FROM proposals WHERE id=? AND user_id=? AND status='pending'`, id, uid).Scan(&d, &title)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	status := "dismissed"
	if accept {
		status = "accepted"
		if t := strings.TrimSpace(editedTitle); t != "" {
			title = t
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO wins(user_id,day,title,source) VALUES(?,?,?,'coach')`, uid, d, title); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proposals SET status=? WHERE id=?`, status, id); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateWinNote updates the optional note for a win.
func (s *Store) UpdateWinNote(ctx context.Context, uid, id int64, note string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wins SET note=? WHERE id=? AND user_id=?`, strings.TrimSpace(note), id, uid)
	return err
}

// AllHabitsWonDays returns habitID -> set of won days since `since`.
func (s *Store) AllHabitsWonDays(ctx context.Context, uid int64, since string) (map[int64]map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT habit_id, day FROM wins WHERE user_id=? AND habit_id IS NOT NULL AND day>=? AND done_at IS NOT NULL`, uid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[string]bool{}
	for rows.Next() {
		var hid int64
		var d string
		if err := rows.Scan(&hid, &d); err != nil {
			return nil, err
		}
		if out[hid] == nil {
			out[hid] = map[string]bool{}
		}
		out[hid][d] = true
	}
	return out, rows.Err()
}

// MemoryNote is an editable plain-text fact retained across time.
type MemoryNote struct {
	ID        int64
	Content   string
	CreatedAt string
}

// ListMemoryNotes returns all editable memory notes for uid.
func (s *Store) ListMemoryNotes(ctx context.Context, uid int64) ([]MemoryNote, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, content, created_at FROM memory_notes WHERE user_id=? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryNote
	for rows.Next() {
		var m MemoryNote
		if err := rows.Scan(&m.ID, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMemoryNote adds a memory fact.
func (s *Store) AddMemoryNote(ctx context.Context, uid int64, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO memory_notes(user_id, content) VALUES(?,?)`, uid, content)
	return err
}

// UpdateMemoryNote edits an existing memory note.
func (s *Store) UpdateMemoryNote(ctx context.Context, uid, id int64, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return s.DeleteMemoryNote(ctx, uid, id)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE memory_notes SET content=? WHERE id=? AND user_id=?`, content, id, uid)
	return err
}

// DeleteMemoryNote deletes a memory note.
func (s *Store) DeleteMemoryNote(ctx context.Context, uid, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM memory_notes WHERE id=? AND user_id=?`, id, uid)
	return err
}

// MemoryNotes returns the user's long-term memory facts as plain strings for prompt context.
func (s *Store) MemoryNotes(ctx context.Context, uid int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content FROM memory_notes WHERE user_id=? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PushSubscription stores a Web Push client registration.
type PushSubscription struct {
	ID       int64
	UserID   int64
	Endpoint string
	P256DH   string
	Auth     string
}

// SavePushSubscription stores or updates a Web Push subscription.
func (s *Store) SavePushSubscription(ctx context.Context, uid int64, endpoint, p256dh, auth string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO push_subscriptions(user_id, endpoint, p256dh, auth)
		VALUES(?,?,?,?) ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id, p256dh=excluded.p256dh, auth=excluded.auth`,
		uid, endpoint, p256dh, auth)
	return err
}

// ListPushSubscriptions returns all push subscriptions for a user.
func (s *Store) ListPushSubscriptions(ctx context.Context, uid int64) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, endpoint, p256dh, auth FROM push_subscriptions WHERE user_id=?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256DH, &p.Auth); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePushSubscription removes a push subscription.
func (s *Store) DeletePushSubscription(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint=?`, endpoint)
	return err
}


// ---- LLM budget ----------------------------------------------------------

// MonthCostMicros returns spend (in millionths of a USD) for month "YYYY-MM".
func (s *Store) MonthCostMicros(ctx context.Context, month string) (int64, error) {
	var c int64
	err := s.db.QueryRowContext(ctx, `SELECT cost_micros FROM llm_usage WHERE month=?`, month).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return c, err
}

// AddCostMicros records spend.
func (s *Store) AddCostMicros(ctx context.Context, month string, micros int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO llm_usage(month,cost_micros) VALUES(?,?)
		ON CONFLICT(month) DO UPDATE SET cost_micros=cost_micros+excluded.cost_micros`, month, micros)
	return err
}

// ---- passkeys (WebAuthn) --------------------------------------------------

// SavePasskey stores a webauthn credential for uid.
func (s *Store) SavePasskey(ctx context.Context, uid int64, cred *webauthn.Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	idStr := base64.RawURLEncoding.EncodeToString(cred.ID)
	_, err = s.db.ExecContext(ctx, `INSERT INTO passkey_credentials(id, user_id, public_key, attestation_type, aaguid, sign_count)
		VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET public_key=excluded.public_key, sign_count=excluded.sign_count`,
		idStr, uid, raw, cred.AttestationType, cred.Authenticator.AAGUID, cred.Authenticator.SignCount)
	return err
}

// UserPasskeys returns all webauthn credentials for uid.
func (s *Store) UserPasskeys(ctx context.Context, uid int64) ([]webauthn.Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT public_key FROM passkey_credentials WHERE user_id=?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal(raw, &c); err == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// PasskeyUserByCredentialID finds the user owning a credential.
func (s *Store) PasskeyUserByCredentialID(ctx context.Context, credID []byte) (User, error) {
	idStr := base64.RawURLEncoding.EncodeToString(credID)
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.email, u.tz, u.about FROM passkey_credentials pc JOIN users u ON u.id=pc.user_id WHERE pc.id=?`, idStr).
		Scan(&u.ID, &u.Email, &u.TZ, &u.About)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// UpdatePasskeySignCount updates sign count for replay prevention.
func (s *Store) UpdatePasskeySignCount(ctx context.Context, credID []byte, count uint32) error {
	idStr := base64.RawURLEncoding.EncodeToString(credID)
	_, err := s.db.ExecContext(ctx, `UPDATE passkey_credentials SET sign_count=? WHERE id=?`, count, idStr)
	return err
}

