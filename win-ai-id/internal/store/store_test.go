package store

import (
	"context"
	"path/filepath"
	"testing"

	"winaiid/internal/day"
)

func open(t *testing.T) (*Store, User) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	u, err := s.UpsertUser(context.Background(), "Me@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	return s, u
}

func TestHabitMaterializationIsIdempotentAndScheduled(t *testing.T) {
	ctx := context.Background()
	s, u := open(t)
	_ = s.CreateHabit(ctx, u.ID, "Walk", day.Schedule{Kind: day.Daily})
	_ = s.CreateHabit(ctx, u.ID, "Gym", day.Schedule{Kind: day.Weekdays, WeekdayMask: 1 << 1}) // Mondays
	_ = s.CreateHabit(ctx, u.ID, "Read", day.Schedule{Kind: day.PerWeek, PerWeek: 1})

	mon, tue := "2026-10-05", "2026-10-06"
	for i := 0; i < 2; i++ {
		if err := s.EnsureHabitWins(ctx, u.ID, mon); err != nil {
			t.Fatal(err)
		}
	}
	wins, _ := s.ListWins(ctx, u.ID, mon)
	if len(wins) != 3 {
		t.Fatalf("Monday: want 3 habit wins, got %d", len(wins))
	}
	// Complete "Read" on Monday -> its 1×/week target is met, so not due Tuesday.
	for _, w := range wins {
		if w.Title == "Read" {
			if _, err := s.ToggleWin(ctx, u.ID, w.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = s.EnsureHabitWins(ctx, u.ID, tue)
	wins, _ = s.ListWins(ctx, u.ID, tue)
	if len(wins) != 1 || wins[0].Title != "Walk" {
		t.Fatalf("Tuesday: want only Walk, got %+v", wins)
	}
}

func TestProposalAcceptCreatesCoachWinOnce(t *testing.T) {
	ctx := context.Background()
	s, u := open(t)
	d := "2026-10-07"
	_ = s.AddProposal(ctx, u.ID, d, "Stretch 5 min", "")
	ps, _ := s.PendingProposals(ctx, u.ID, d)
	if len(ps) != 1 {
		t.Fatal("want 1 proposal")
	}
	if err := s.DecideProposal(ctx, u.ID, ps[0].ID, true, "Stretch 10 min"); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideProposal(ctx, u.ID, ps[0].ID, true, ""); err != ErrNotFound {
		t.Fatalf("second decide: want ErrNotFound, got %v", err)
	}
	wins, _ := s.ListWins(ctx, u.ID, d)
	if len(wins) != 1 || wins[0].Title != "Stretch 10 min" || wins[0].Source != "coach" {
		t.Fatalf("got %+v", wins)
	}
}

func TestMagicLinkSingleUse(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	tok, _ := s.CreateMagicLink(ctx, "me@example.com", 60e9)
	if email, err := s.ConsumeMagicLink(ctx, tok); err != nil || email != "me@example.com" {
		t.Fatalf("first use: %v %q", err, email)
	}
	if _, err := s.ConsumeMagicLink(ctx, tok); err != ErrNotFound {
		t.Fatalf("reuse should fail, got %v", err)
	}
}

func TestWinNoteAndMemoryNotes(t *testing.T) {
	ctx := context.Background()
	s, u := open(t)
	d := "2026-10-07"

	// Win note
	winID, err := s.AddWin(ctx, u.ID, d, "Read 10 pages", "manual", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateWinNote(ctx, u.ID, winID, "Chapter 3 was great"); err != nil {
		t.Fatal(err)
	}
	wins, _ := s.ListWins(ctx, u.ID, d)
	if len(wins) != 1 || wins[0].Note != "Chapter 3 was great" {
		t.Fatalf("expected note, got %+v", wins)
	}

	// Memory notes CRUD
	if err := s.AddMemoryNote(ctx, u.ID, "Tends to read before bed"); err != nil {
		t.Fatal(err)
	}
	notes, err := s.ListMemoryNotes(ctx, u.ID)
	if err != nil || len(notes) != 1 || notes[0].Content != "Tends to read before bed" {
		t.Fatalf("unexpected notes: %+v", notes)
	}
	// Update note
	if err := s.UpdateMemoryNote(ctx, u.ID, notes[0].ID, "Tends to read 15m before bed"); err != nil {
		t.Fatal(err)
	}
	rawNotes, _ := s.MemoryNotes(ctx, u.ID)
	if len(rawNotes) != 1 || rawNotes[0] != "Tends to read 15m before bed" {
		t.Fatalf("unexpected raw notes: %+v", rawNotes)
	}
	// Delete note
	if err := s.DeleteMemoryNote(ctx, u.ID, notes[0].ID); err != nil {
		t.Fatal(err)
	}
	notesAfter, _ := s.ListMemoryNotes(ctx, u.ID)
	if len(notesAfter) != 0 {
		t.Fatalf("expected 0 notes after delete, got %d", len(notesAfter))
	}
}

