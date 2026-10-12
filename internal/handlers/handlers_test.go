package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svgamings19-ux/----22/expenses/internal/config"
	"github.com/svgamings19-ux/----22/expenses/internal/models"
	"github.com/svgamings19-ux/----22/expenses/internal/repository"
	"github.com/svgamings19-ux/----22/expenses/web"
	"golang.org/x/crypto/bcrypt"
)

type browser struct {
	h       http.Handler
	cookies map[string]*http.Cookie
}

func (b *browser) request(method, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}
func (b *browser) post(path string, values url.Values) *httptest.ResponseRecorder {
	if values == nil {
		values = url.Values{}
	}
	if c := b.cookies[csrfCookie]; c != nil {
		values.Set("csrf_token", c.Value)
	}
	return b.request("POST", path, values)
}
func testApp(t *testing.T) (*browser, *repository.ExpenseRepository, *bytes.Buffer) {
	t.Helper()
	repo, err := repository.Open(filepath.Join(t.TempDir(), "expenses.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	cfg := config.Config{SessionSecret: strings.Repeat("test-secret-", 4), SessionTTL: time.Hour}
	logs := &bytes.Buffer{}
	h, err := New(repo, cfg, log.New(logs, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	return &browser{h: h, cookies: map[string]*http.Cookie{}}, repo, logs
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
}
func signup(t *testing.T, b *browser, email string) {
	t.Helper()
	status(t, b.request("GET", "/register", nil), 200)
	status(t, b.post("/register", url.Values{"email": {email}, "password": {"password123"}}), 303)
	status(t, b.post("/login", url.Values{"email": {email}, "password": {"password123"}}), 303)
	status(t, b.request("GET", "/expenses", nil), 200)
}
func expenseValues(description string) url.Values {
	return url.Values{"amount": {"123,45"}, "description": {description}, "date": {"2026-01-15"}, "category_id": {"1"}}
}

func TestFullFlowIsolationStatsExportAndLogout(t *testing.T) {
	a, repo, logs := testApp(t)
	for _, path := range []string{"/", "/expenses", "/expenses/new", "/stats", "/expenses/export"} {
		w := a.request("GET", path, nil)
		status(t, w, 303)
		if w.Header().Get("Location") != "/login" {
			t.Fatal("missing auth redirect")
		}
	}
	status(t, a.request("GET", "/api/stats/by-category", nil), 401)
	signup(t, a, "Alice@Example.com")
	cookie := a.cookies[sessionCookie]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 3600 {
		t.Fatalf("cookie: %+v", cookie)
	}
	u, err := repo.UserByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if string(u.PasswordHash) == "password123" || bcrypt.CompareHashAndPassword(u.PasswordHash, []byte("password123")) != nil {
		t.Fatal("password not hashed")
	}
	text := "<script>alert(1)</script>"
	w := a.post("/expenses/new", expenseValues(text))
	status(t, w, 303)
	if w.Header().Get("Location") != "/expenses" {
		t.Fatal("missing PRG")
	}
	for i := 0; i < 2; i++ {
		w = a.request("GET", "/expenses", nil)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), "&lt;script&gt;") || strings.Contains(w.Body.String(), text) {
			t.Fatal("XSS not escaped")
		}
	}
	rows, err := repo.GetAll(context.Background(), u.ID, models.Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	id := rows[0].ID
	b := &browser{h: a.h, cookies: map[string]*http.Cookie{}}
	signup(t, b, "bob@example.com")
	for _, path := range []string{fmt.Sprintf("/expenses/%d/edit", id), fmt.Sprintf("/expenses/%d/delete", id)} {
		status(t, b.request("GET", path, nil), 404)
		status(t, b.post(path, expenseValues("stolen")), 404)
	}
	if strings.Contains(b.request("GET", "/expenses", nil).Body.String(), "&lt;script&gt;") {
		t.Fatal("foreign expense visible")
	}
	bw := b.request("GET", "/api/stats/by-category", nil)
	status(t, bw, 200)
	if strings.TrimSpace(bw.Body.String()) != "[]" {
		t.Fatal("foreign stats")
	}
	if strings.Contains(b.request("GET", "/expenses/export", nil).Body.String(), "alert(1)") {
		t.Fatal("foreign CSV")
	}
	for _, path := range []string{"/expenses?category=1&from=2026-01-15&to=2026-01-15", "/expenses?category=1", "/expenses?from=2026-01-01", "/expenses?to=2026-01-31"} {
		w = a.request("GET", path, nil)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), "123,45") {
			t.Fatal("filter lost expense")
		}
	}
	w = a.request("GET", "/api/stats/by-category?from=2026-01-01&to=2026-01-31", nil)
	status(t, w, 200)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("JSON content type")
	}
	var stats []models.Stat
	if err = json.Unmarshal(w.Body.Bytes(), &stats); err != nil || len(stats) != 1 || stats[0].Amount != 12345 {
		t.Fatal(stats, err)
	}
	w = a.request("GET", "/api/stats/by-month?year=2026", nil)
	status(t, w, 200)
	if err = json.Unmarshal(w.Body.Bytes(), &stats); err != nil || len(stats) != 12 || stats[0].Amount != 12345 {
		t.Fatal(stats, err)
	}
	update := expenseValues("=HYPERLINK(\"http://example.test\")")
	update.Set("amount", "15.01")
	status(t, a.post(fmt.Sprintf("/expenses/%d/edit", id), update), 303)
	w = a.request("GET", "/expenses/export?category=1", nil)
	status(t, w, 200)
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment; filename=\"expenses-") {
		t.Fatal("download header")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff")))
	reader.Comma = ';'
	records, err := reader.ReadAll()
	if err != nil || len(records) != 2 || records[1][3] != "15,01" || !strings.HasPrefix(records[1][2], "'=") {
		t.Fatal(records, err)
	}
	status(t, a.request("GET", fmt.Sprintf("/expenses/%d/delete", id), nil), 200)
	rows, _ = repo.GetAll(context.Background(), u.ID, models.Filter{})
	if len(rows) != 1 {
		t.Fatal("GET deleted record")
	}
	status(t, a.post(fmt.Sprintf("/expenses/%d/delete", id), nil), 303)
	status(t, a.request("GET", fmt.Sprintf("/expenses/%d/edit", id), nil), 404)
	old := *a.cookies[sessionCookie]
	status(t, a.post("/logout", nil), 303)
	a.cookies[sessionCookie] = &old
	status(t, a.request("GET", "/expenses", nil), 303)
	if !strings.Contains(logs.String(), "status=303") || !strings.Contains(logs.String(), "duration=") {
		t.Fatal("request logs missing")
	}
}

func TestValidationCSRFAndSessionTampering(t *testing.T) {
	b, repo, _ := testApp(t)
	signup(t, b, "valid@example.com")
	for _, tc := range []struct{ key, value string }{{"amount", "-1"}, {"amount", "0"}, {"amount", "NaN"}, {"amount", "1.234"}, {"description", " "}, {"description", strings.Repeat("я", 201)}, {"date", "2026-02-30"}, {"date", "0000-01-01"}, {"category_id", "999"}, {"category_id", "1 OR 1=1"}} {
		v := expenseValues("preserve this")
		v.Set(tc.key, tc.value)
		w := b.post("/expenses/new", v)
		status(t, w, 400)
		if !strings.Contains(w.Body.String(), "role=\"alert\"") {
			t.Fatal("missing form error")
		}
		if tc.key != "description" && !strings.Contains(w.Body.String(), "preserve this") {
			t.Fatal("lost form values")
		}
	}
	u, _ := repo.UserByEmail(context.Background(), "valid@example.com")
	rows, _ := repo.GetAll(context.Background(), u.ID, models.Filter{})
	if len(rows) != 0 {
		t.Fatal("invalid data saved")
	}
	status(t, b.request("POST", "/expenses/new", expenseValues("without CSRF")), 403)
	token := b.cookies[csrfCookie].Value
	values := expenseValues("bad token")
	values.Set("csrf_token", token+"x")
	status(t, b.request("POST", "/expenses/new", values), 403)
	forged := &browser{h: b.h, cookies: map[string]*http.Cookie{sessionCookie: {Name: sessionCookie, Value: b.cookies[sessionCookie].Value + "x"}}}
	status(t, forged.request("GET", "/expenses", nil), 303)
	oversized := expenseValues(strings.Repeat("x", 17000))
	status(t, b.post("/expenses/new", oversized), 400)
	for _, path := range []string{"/expenses?from=2026-02-30", "/expenses?from=2026-02-02&to=2026-01-01", "/expenses?category=999", "/api/stats/by-month?year=0", "/api/stats/by-month?year=no", "/api/stats/by-category?from=invalid", "/expenses/export?category=-1"} {
		status(t, b.request("GET", path, nil), 400)
	}
}

func TestAuthErrorsAndExpiredSession(t *testing.T) {
	b, repo, _ := testApp(t)
	signup(t, b, "one@example.com")
	other := &browser{h: b.h, cookies: map[string]*http.Cookie{}}
	status(t, other.request("GET", "/register", nil), 200)
	status(t, other.post("/register", url.Values{"email": {"ONE@example.com"}, "password": {"password123"}}), 409)
	status(t, other.post("/register", url.Values{"email": {"not-email"}, "password": {"short"}}), 400)
	status(t, other.post("/register", url.Values{"email": {"two@example.com"}, "password": {strings.Repeat("я", 37)}}), 400)
	status(t, other.post("/login", url.Values{"email": {"one@example.com"}, "password": {"wrongpassword"}}), 401)
	status(t, other.post("/login", url.Values{"email": {"missing@example.com"}, "password": {"wrongpassword"}}), 401)
	u, _ := repo.UserByEmail(context.Background(), "one@example.com")
	h := &Handler{cfg: config.Config{SessionSecret: strings.Repeat("test-secret-", 4)}}
	if err := repo.CreateSession(context.Background(), tokenHash("expired"), u.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	b.cookies[sessionCookie] = &http.Cookie{Name: sessionCookie, Value: h.sign("session", "expired")}
	status(t, b.request("GET", "/expenses", nil), 303)
	status(t, other.request("POST", "/register", url.Values{"email": {"no-csrf@example.com"}, "password": {"password123"}}), 403)
}

func TestRoutesAndRecovery(t *testing.T) {
	b, _, logs := testApp(t)
	for _, tc := range []struct {
		method, path, text string
		code               int
	}{{"GET", "/about", "Учёт личных трат", 200}, {"GET", "/ping", "pong", 200}, {"POST", "/ping", "", 405}, {"HEAD", "/ping", "", 405}, {"GET", "/missing", "404", 404}, {"GET", "/static/style.css", "font-family", 200}, {"GET", "/login", "Вход", 200}} {
		w := b.request(tc.method, tc.path, nil)
		status(t, w, tc.code)
		if !strings.Contains(w.Body.String(), tc.text) {
			t.Fatal("missing page text")
		}
	}
	pages := map[string]*template.Template{"500": template.Must(template.ParseFS(web.Assets, "templates/layout.html", "templates/500.html"))}
	h := &Handler{logger: log.New(logs, "", 0), pages: pages}
	recovered := h.logging(h.recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("test panic") })))
	w := httptest.NewRecorder()
	recovered.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	status(t, w, 500)
	if strings.Contains(w.Body.String(), "test panic") || !strings.Contains(w.Body.String(), "Что-то пошло не так") {
		t.Fatal("wrong public error")
	}
	if !strings.Contains(logs.String(), "status=500") {
		t.Fatal("panic status not logged")
	}
	status(t, b.request("GET", "/ping", nil), 200)
}

func TestAmountsAndCSVText(t *testing.T) {
	for raw, want := range map[string]int64{"0.01": 1, "12,50": 1250, "5": 500, "1.2": 120, "999999999.99": 99999999999} {
		got, err := parseAmount(raw)
		if err != nil || got != want {
			t.Fatal(raw, got, err)
		}
	}
	for _, raw := range []string{"", "Inf", "1e3", "1.2.3", "1000000000", "1.", "-0.01", "0", strings.Repeat("9", 100)} {
		if _, err := parseAmount(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	for _, s := range []string{"=1+1", " +cmd", "-2", "@foo", "\t=1", "\ntext"} {
		if !strings.HasPrefix(csvText(s), "'") {
			t.Fatal("unsafe CSV", s)
		}
	}
	if csvText("Обычная покупка") != "Обычная покупка" {
		t.Fatal("changed ordinary text")
	}
}

func TestRateLimitAndSecureCookie(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Now()
	for i := 0; i < 20; i++ {
		if !limiter.allow("127.0.0.1:1000", now) {
			t.Fatal("early limit")
		}
	}
	if limiter.allow("127.0.0.1:2000", now) {
		t.Fatal("limit bypass by changing port")
	}
	if !limiter.allow("127.0.0.1:1000", now.Add(16*time.Minute)) {
		t.Fatal("limit never expires")
	}
	h := &Handler{cfg: config.Config{CookieSecure: true}, logger: log.New(io.Discard, "", 0)}
	w := httptest.NewRecorder()
	h.cookie(w, sessionCookie, "token", time.Hour)
	c := w.Result().Cookies()[0]
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatal(c)
	}
}
