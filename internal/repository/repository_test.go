package repository

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/svgamings19-ux/----22/expenses/internal/models"
)

func TestPersistenceCRUDIsolationAndFilters(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "expenses.db")
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { r.Close() }()
	u1, err := r.CreateUser(ctx, "one@example.com", []byte("hash"))
	if err != nil {
		t.Fatal(err)
	}
	u2, err := r.CreateUser(ctx, "two@example.com", []byte("hash"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CreateUser(ctx, "ONE@example.com", []byte("hash")); !errors.Is(err, ErrEmailExists) {
		t.Fatalf("duplicate email: %v", err)
	}
	cats, err := r.Categories(ctx)
	if err != nil || len(cats) != 7 {
		t.Fatalf("categories: %v %v", cats, err)
	}
	expenses := []models.Expense{{Amount: 101, Description: "A", Date: "2026-01-01", CategoryID: 1}, {Amount: 299, Description: "B", Date: "2026-01-31", CategoryID: 2}, {Amount: 500, Description: "C", Date: "2026-02-01", CategoryID: 1}, {Amount: 999, Description: "D", Date: "2025-12-31", CategoryID: 1}}
	var id int64
	for i, e := range expenses {
		created, err := r.Create(ctx, u1, e)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			id = created
		}
	}
	if _, err := r.Create(ctx, u2, models.Expense{Amount: 80000, Description: "private", Date: "2026-01-01", CategoryID: 1}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		f    models.Filter
		want int
	}{{models.Filter{}, 4}, {models.Filter{CategoryID: 1}, 3}, {models.Filter{From: "2026-01-01"}, 3}, {models.Filter{To: "2026-01-31"}, 3}, {models.Filter{From: "2026-01-01", To: "2026-01-31"}, 2}, {models.Filter{CategoryID: 1, From: "2026-01-01", To: "2026-01-31"}, 1}, {models.Filter{From: "2030-01-01"}, 0}} {
		got, err := r.GetAll(ctx, u1, tc.f)
		if err != nil || len(got) != tc.want {
			t.Fatalf("filter %+v: got %d, %v", tc.f, len(got), err)
		}
		for _, e := range got {
			if e.UserID != u1 || e.Category == "" {
				t.Fatal("missing ownership or JOIN")
			}
		}
	}
	if _, err = r.GetByID(ctx, u2, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign read allowed")
	}
	if err = r.Update(ctx, u2, id, expenses[1]); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign update allowed")
	}
	if err = r.Delete(ctx, u2, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign delete allowed")
	}
	e, err := r.GetByID(ctx, u1, id)
	if err != nil || e.Amount != 101 {
		t.Fatal(e, err)
	}
	e.Description = "'; DROP TABLE expenses; --"
	e.Amount = 102
	if err = r.CreateSession(ctx, "persisted-session-hash", u1, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = r.Update(ctx, u1, id, e); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if user, err := r.SessionUser(ctx, "persisted-session-hash", time.Now()); err != nil || user.ID != u1 {
		t.Fatal("session lost after restart", user, err)
	}
	e, err = r.GetByID(ctx, u1, id)
	if err != nil || e.Amount != 102 || e.Description != "'; DROP TABLE expenses; --" {
		t.Fatal("persistence failed", e, err)
	}
	cats, err = r.Categories(ctx)
	if err != nil || len(cats) != 7 {
		t.Fatal("migration not idempotent")
	}
	if err = r.Delete(ctx, u1, id); err != nil {
		t.Fatal(err)
	}
	if err = r.Delete(ctx, u1, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted row found")
	}
	if _, err = r.Create(ctx, u1, models.Expense{Amount: 1, Description: "invalid FK", Date: "2026-01-01", CategoryID: 999}); err == nil {
		t.Fatal("foreign key not enforced")
	}
	if _, err = r.Create(ctx, u1, models.Expense{Amount: -1, Description: "invalid amount", Date: "2026-01-01", CategoryID: 1}); err == nil {
		t.Fatal("amount constraint not enforced")
	}
}

func TestStatisticsAndSessions(t *testing.T) {
	ctx := context.Background()
	r, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	u, _ := r.CreateUser(ctx, "user@example.com", []byte("hash"))
	other, _ := r.CreateUser(ctx, "other@example.com", []byte("hash"))
	for _, v := range []struct {
		u int64
		e models.Expense
	}{{u, models.Expense{Amount: 101, Description: "a", Date: "2026-01-01", CategoryID: 1}}, {u, models.Expense{Amount: 299, Description: "b", Date: "2026-01-31", CategoryID: 1}}, {u, models.Expense{Amount: 700, Description: "c", Date: "2026-02-01", CategoryID: 2}}, {other, models.Expense{Amount: 900000, Description: "secret", Date: "2026-01-01", CategoryID: 1}}} {
		if _, err := r.Create(ctx, v.u, v.e); err != nil {
			t.Fatal(err)
		}
	}
	byCat, err := r.ByCategory(ctx, u, models.Filter{From: "2026-01-01", To: "2026-01-31"})
	if err != nil || len(byCat) != 1 || byCat[0].Amount != 400 || byCat[0].Count != 2 {
		t.Fatalf("category stats: %+v %v", byCat, err)
	}
	byMonth, err := r.ByMonth(ctx, u, 2026)
	if err != nil || len(byMonth) != 12 || byMonth[0].Amount != 400 || byMonth[1].Amount != 700 || byMonth[11].Amount != 0 {
		t.Fatalf("monthly: %+v %v", byMonth, err)
	}
	empty, err := r.ByCategory(ctx, u, models.Filter{From: "2040-01-01"})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty JSON must be []")
	}
	now := time.Now().Truncate(time.Second)
	if err := r.CreateSession(ctx, "hash", u, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := r.SessionUser(ctx, "hash", now); err != nil || got.ID != u {
		t.Fatal(got, err)
	}
	if _, err := r.SessionUser(ctx, "hash", now.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session accepted")
	}
	if err := r.DeleteSession(ctx, "hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SessionUser(ctx, "hash", now); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked session accepted")
	}
}

func TestConcurrentCreates(t *testing.T) {
	ctx := context.Background()
	r, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	u, err := r.CreateUser(ctx, "user@example.com", []byte("hash"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Create(ctx, u, models.Expense{Amount: 1, Description: "parallel", Date: "2026-01-01", CategoryID: 1}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rows, err := r.GetAll(ctx, u, models.Filter{})
	if err != nil || len(rows) != 20 {
		t.Fatal(len(rows), err)
	}
}
