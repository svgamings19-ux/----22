package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func request(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	h, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestRoutes(t *testing.T) {
	h := newHandler(t)
	for _, tc := range []struct {
		method, path, text string
		status             int
	}{
		{"GET", "/", "Пока нет трат", 200}, {"GET", "/about", "О проекте", 200},
		{"GET", "/expenses/new", "Сохранить трату", 200}, {"GET", "/ping", "pong", 200},
		{"POST", "/ping", "", 405}, {"HEAD", "/ping", "", 405}, {"GET", "/missing", "", 404},
		{"GET", "/static/style.css", "font-family", 200}, {"DELETE", "/expenses/new", "", 405},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := request(h, tc.method, tc.path, "")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.text) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.status == 405 && w.Header().Get("Allow") == "" {
				t.Fatal("missing Allow")
			}
		})
	}
}

func TestCreateRedirectEscapeAndReset(t *testing.T) {
	h := newHandler(t)
	payload := url.Values{"amount": {"123.45"}, "description": {"<script>alert(1)</script>"}, "date": {"2026-09-28"}}.Encode()
	w := request(h, "POST", "/expenses/new", payload)
	if w.Code != 303 || w.Header().Get("Location") != "/expenses" {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	for i := 0; i < 2; i++ { // Refresh must not create another row.
		body := request(h, "GET", "/expenses", "").Body.String()
		for _, s := range []string{"123,45", "28.09.2026", "Записей: 1", "&lt;script&gt;"} {
			if !strings.Contains(body, s) {
				t.Fatalf("missing %s", s)
			}
		}
		if strings.Contains(body, "<script>alert") {
			t.Fatal("unescaped HTML")
		}
	}
	if !strings.Contains(request(newHandler(t), "GET", "/expenses", "").Body.String(), "Пока нет трат") {
		t.Fatal("state persisted unexpectedly")
	}
}

func TestInvalidFormsDoNotAddExpenses(t *testing.T) {
	for _, tc := range []struct{ amount, description, date string }{
		{"-1", "Test", "2026-09-28"}, {"NaN", "Test", "2026-09-28"}, {"1.234", "Test", "2026-09-28"},
		{"10", " ", "2026-09-28"}, {"10", "Test", "2026-02-30"}, {"0", "Test", "2026-09-28"},
	} {
		h := newHandler(t)
		w := request(h, "POST", "/expenses/new", url.Values{"amount": {tc.amount}, "description": {tc.description}, "date": {tc.date}}.Encode())
		if w.Code != 400 || !strings.Contains(w.Body.String(), "role=\"alert\"") {
			t.Fatalf("expected form error: %d", w.Code)
		}
		if !strings.Contains(request(h, "GET", "/expenses", "").Body.String(), "Пока нет трат") {
			t.Fatal("invalid expense added")
		}
	}
}

func TestAmounts(t *testing.T) {
	for raw, want := range map[string]int64{"0.01": 1, "12,50": 1250, "5": 500, "1.2": 120, "999999999.99": 99999999999} {
		got, err := parseAmount(raw)
		if err != nil || got != want {
			t.Fatalf("%s => %d, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "Inf", "1e3", "1.2.3", "1000000000", "1.", "-0.01"} {
		if _, err := parseAmount(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	h := newHandler(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(h, "POST", "/expenses/new", "amount=1&description=Test&date=2026-09-28")
			if w.Code != 303 {
				t.Errorf("POST: %d", w.Code)
			}
			request(h, "GET", "/expenses", "")
		}()
	}
	wg.Wait()
	if !strings.Contains(request(h, "GET", "/expenses", "").Body.String(), "Записей: 20") {
		t.Fatal("lost expenses")
	}
}
