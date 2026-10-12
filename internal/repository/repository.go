package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/svgamings19-ux/----22/expenses/internal/models"
	"github.com/svgamings19-ux/----22/expenses/migrations"
	"modernc.org/sqlite"
)

var ErrNotFound = errors.New("record not found")
var ErrEmailExists = errors.New("email already registered")

type ExpenseRepository struct{ db *sql.DB }

func Open(path string) (*ExpenseRepository, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection ensures PRAGMAs also apply to all subsequent queries.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	r := &ExpenseRepository{db: db}
	for _, query := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
		if _, err = db.Exec(query); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = r.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *ExpenseRepository) Close() error { return r.db.Close() }

func (r *ExpenseRepository) migrate() error {
	if _, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}
		var exists int
		err = tx.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name).Scan(&exists)
		if err != nil {
			tx.Rollback()
			return err
		}
		if exists != 0 {
			tx.Rollback()
			continue
		}
		script, err := migrations.Files.ReadFile(name)
		if err == nil {
			_, err = tx.Exec(string(script))
		}
		if err == nil {
			_, err = tx.Exec("INSERT INTO schema_migrations(name) VALUES (?)", name)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (r *ExpenseRepository) Categories(ctx context.Context) ([]models.Category, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, name FROM categories ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Category{}
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func conditions(userID int64, f models.Filter) (string, []any) {
	q := " WHERE e.user_id = ?"
	args := []any{userID}
	if f.CategoryID != 0 {
		q += " AND e.category_id = ?"
		args = append(args, f.CategoryID)
	}
	if f.From != "" {
		q += " AND e.date >= ?"
		args = append(args, f.From)
	}
	if f.To != "" {
		q += " AND e.date <= ?"
		args = append(args, f.To)
	}
	return q, args
}

const expenseSelect = `SELECT e.id, e.user_id, e.amount, e.description, e.date, e.category_id, c.name FROM expenses e JOIN categories c ON c.id = e.category_id`

func (r *ExpenseRepository) GetAll(ctx context.Context, userID int64, f models.Filter) ([]models.Expense, error) {
	where, args := conditions(userID, f)
	rows, err := r.db.QueryContext(ctx, expenseSelect+where+" ORDER BY e.date DESC, e.id DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Expense{}
	for rows.Next() {
		var e models.Expense
		if err := rows.Scan(&e.ID, &e.UserID, &e.Amount, &e.Description, &e.Date, &e.CategoryID, &e.Category); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *ExpenseRepository) GetByID(ctx context.Context, userID, id int64) (models.Expense, error) {
	var e models.Expense
	err := r.db.QueryRowContext(ctx, expenseSelect+" WHERE e.user_id = ? AND e.id = ?", userID, id).Scan(&e.ID, &e.UserID, &e.Amount, &e.Description, &e.Date, &e.CategoryID, &e.Category)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return e, err
}

func (r *ExpenseRepository) Create(ctx context.Context, userID int64, e models.Expense) (int64, error) {
	res, err := r.db.ExecContext(ctx, "INSERT INTO expenses(user_id,amount,description,date,category_id) VALUES (?,?,?,?,?)", userID, e.Amount, e.Description, e.Date, e.CategoryID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func changed(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *ExpenseRepository) Update(ctx context.Context, userID, id int64, e models.Expense) error {
	return changed(r.db.ExecContext(ctx, "UPDATE expenses SET amount=?,description=?,date=?,category_id=? WHERE user_id=? AND id=?", e.Amount, e.Description, e.Date, e.CategoryID, userID, id))
}
func (r *ExpenseRepository) Delete(ctx context.Context, userID, id int64) error {
	return changed(r.db.ExecContext(ctx, "DELETE FROM expenses WHERE user_id=? AND id=?", userID, id))
}

func (r *ExpenseRepository) CreateUser(ctx context.Context, email string, hash []byte) (int64, error) {
	res, err := r.db.ExecContext(ctx, "INSERT INTO users(email,password_hash) VALUES (?,?)", strings.ToLower(email), hash)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == 2067 {
			return 0, ErrEmailExists
		}
		return 0, err
	}
	return res.LastInsertId()
}
func (r *ExpenseRepository) UserByEmail(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := r.db.QueryRowContext(ctx, "SELECT id,email,password_hash FROM users WHERE email=?", email).Scan(&u.ID, &u.Email, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return u, err
}
func (r *ExpenseRepository) CreateSession(ctx context.Context, hash string, userID int64, expires time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(session_id,user_id,expires_at) VALUES (?,?,?)", hash, userID, expires.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *ExpenseRepository) SessionUser(ctx context.Context, hash string, now time.Time) (models.User, error) {
	var u models.User
	err := r.db.QueryRowContext(ctx, `SELECT u.id,u.email FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.session_id=? AND s.expires_at>?`, hash, now.Unix()).Scan(&u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return u, err
}
func (r *ExpenseRepository) DeleteSession(ctx context.Context, hash string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM sessions WHERE session_id=?", hash)
	return err
}
func (r *ExpenseRepository) ByCategory(ctx context.Context, userID int64, f models.Filter) ([]models.Stat, error) {
	where, args := conditions(userID, f)
	return r.stats(ctx, `SELECT c.name,SUM(e.amount),COUNT(*) FROM expenses e JOIN categories c ON c.id=e.category_id`+where+` GROUP BY c.id,c.name ORDER BY SUM(e.amount) DESC,c.id`, args...)
}
func (r *ExpenseRepository) ByMonth(ctx context.Context, userID int64, year int) ([]models.Stat, error) {
	rows, err := r.stats(ctx, `SELECT substr(date,1,7),SUM(amount),COUNT(*) FROM expenses WHERE user_id=? AND date>=? AND date<=? GROUP BY substr(date,1,7) ORDER BY 1`, userID, fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-12-31", year))
	if err != nil {
		return nil, err
	}
	out := make([]models.Stat, 12)
	for i := range out {
		out[i].Label = fmt.Sprintf("%04d-%02d", year, i+1)
		for _, s := range rows {
			if s.Label == out[i].Label {
				out[i] = s
				break
			}
		}
	}
	return out, nil
}
func (r *ExpenseRepository) stats(ctx context.Context, q string, args ...any) ([]models.Stat, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Stat{}
	for rows.Next() {
		var s models.Stat
		if err := rows.Scan(&s.Label, &s.Amount, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
