package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/svgamings19-ux/----22/expenses/internal/models"
	"github.com/svgamings19-ux/----22/expenses/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type contextKey uint8

const (
	userKey contextKey = iota
	csrfKey
)
const sessionCookie = "expenses_session"
const csrfCookie = "expenses_csrf"

func currentUser(r *http.Request) models.User {
	u, _ := r.Context().Value(userKey).(models.User)
	return u
}
func csrfValue(r *http.Request) string { v, _ := r.Context().Value(csrfKey).(string); return v }
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenHash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func (h *Handler) sign(purpose, value string) string {
	m := hmac.New(sha256.New, []byte(h.cfg.SessionSecret))
	m.Write([]byte(purpose + ":" + value))
	return value + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (h *Handler) verify(purpose, signed string) (string, bool) {
	if len(signed) > 256 {
		return "", false
	}
	i := strings.LastIndexByte(signed, '.')
	if i < 0 {
		return "", false
	}
	value := signed[:i]
	expected := h.sign(purpose, value)
	return value, hmac.Equal([]byte(expected), []byte(signed))
}
func (h *Handler) cookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	maxAge := int(ttl.Seconds())
	expires := time.Now().Add(ttl)
	if ttl < 0 {
		maxAge = -1
		expires = time.Unix(1, 0)
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: h.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge, Expires: expires})
}
func (h *Handler) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			if token, ok := h.verify("session", c.Value); ok {
				u, err := h.repo.SessionUser(r.Context(), tokenHash(token), time.Now())
				if err == nil {
					r = r.WithContext(context.WithValue(r.Context(), userKey, u))
				} else if !errors.Is(err, repository.ErrNotFound) {
					h.dbError(w, r, err)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).ID == 0 {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				h.fail(w, r, 401, "Войдите в аккаунт.")
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) validCSRF(value string) bool {
	token, ok := h.verify("csrf", value)
	if !ok {
		return false
	}
	parts := strings.Split(token, ":")
	if len(parts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && expires > time.Now().Unix()
}
func (h *Handler) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/ping" {
			next.ServeHTTP(w, r)
			return
		}
		var token string
		if c, err := r.Cookie(csrfCookie); err == nil && h.validCSRF(c.Value) {
			token = c.Value
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
			if err := r.ParseForm(); err != nil {
				h.fail(w, r, 400, "Не удалось прочитать форму: слишком большой или некорректный запрос.")
				return
			}
			if token == "" || !hmac.Equal([]byte(token), []byte(r.PostFormValue("csrf_token"))) {
				h.fail(w, r, 403, "Форма устарела. Обновите страницу и повторите действие.")
				return
			}
		} else if token == "" {
			raw, err := randomToken()
			if err != nil {
				h.dbError(w, r, err)
				return
			}
			token = h.sign("csrf", raw+":"+strconv.FormatInt(time.Now().Add(12*time.Hour).Unix(), 10))
			h.cookie(w, csrfCookie, token, 12*time.Hour)
		}
		r = r.WithContext(context.WithValue(r.Context(), csrfKey, token))
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) prepareAuth() error {
	var err error
	h.dummyHash, err = bcrypt.GenerateFromPassword([]byte("not-a-real-account-password"), bcrypt.DefaultCost)
	return err
}
func (h *Handler) authForm(w http.ResponseWriter, r *http.Request) {
	if currentUser(r).ID != 0 {
		http.Redirect(w, r, "/expenses", 303)
		return
	}
	d := pageData{Title: "Вход", Mode: "login", Action: "/login"}
	if r.URL.Query().Get("registered") == "1" {
		d.Notice = "Аккаунт создан. Войдите с вашим email и паролем."
	}
	if r.URL.Path == "/register" {
		d.Title = "Регистрация"
		d.Mode = "register"
		d.Action = "/register"
	}
	h.render(w, r, "auth", 200, d)
}
func (h *Handler) authInput(w http.ResponseWriter, r *http.Request, mode string) (pageData, string, bool) {
	d := pageData{Title: "Вход", Mode: mode, Action: "/" + mode, Email: strings.ToLower(strings.TrimSpace(r.PostFormValue("email"))), Errors: map[string]string{}}
	if mode == "register" {
		d.Title = "Регистрация"
	}
	if !h.limiter.allow(r.RemoteAddr, time.Now()) {
		w.Header().Set("Retry-After", "900")
		h.fail(w, r, 429, "Слишком много попыток. Повторите через 15 минут.")
		return d, "", false
	}
	password := r.PostFormValue("password")
	addr, err := mail.ParseAddress(d.Email)
	if err != nil || addr.Address != d.Email || len(d.Email) > 254 {
		d.Errors["email"] = "Введите корректный email без имени и пробелов."
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 8 || len(password) > 72 {
		d.Errors["password"] = "Пароль: минимум 8 символов, максимум 72 байта UTF-8."
	}
	if len(d.Errors) > 0 {
		d.Error = "Проверьте поля формы."
		h.render(w, r, "auth", 400, d)
		return d, "", false
	}
	return d, password, true
}
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	d, password, ok := h.authInput(w, r, "register")
	if !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	_, err = h.repo.CreateUser(r.Context(), d.Email, hash)
	if errors.Is(err, repository.ErrEmailExists) {
		d.Error = "Этот email уже зарегистрирован. Перейдите ко входу."
		h.render(w, r, "auth", 409, d)
		return
	}
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	http.Redirect(w, r, "/login?registered=1", 303)
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	d, password, ok := h.authInput(w, r, "login")
	if !ok {
		return
	}
	u, err := h.repo.UserByEmail(r.Context(), d.Email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		h.dbError(w, r, err)
		return
	}
	hash := u.PasswordHash
	if errors.Is(err, repository.ErrNotFound) {
		hash = h.dummyHash
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || u.ID == 0 {
		d.Error = "Неверный email или пароль."
		h.render(w, r, "auth", 401, d)
		return
	}
	if c, e := r.Cookie(sessionCookie); e == nil {
		if old, valid := h.verify("session", c.Value); valid {
			if e = h.repo.DeleteSession(r.Context(), tokenHash(old)); e != nil {
				h.dbError(w, r, e)
				return
			}
		}
	}
	token, err := randomToken()
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	if err = h.repo.CreateSession(r.Context(), tokenHash(token), u.ID, time.Now().Add(h.cfg.SessionTTL)); err != nil {
		h.dbError(w, r, err)
		return
	}
	h.cookie(w, sessionCookie, h.sign("session", token), h.cfg.SessionTTL)
	h.cookie(w, csrfCookie, "", -time.Second)
	http.Redirect(w, r, "/expenses", 303)
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if token, ok := h.verify("session", c.Value); ok {
			if err = h.repo.DeleteSession(r.Context(), tokenHash(token)); err != nil {
				h.dbError(w, r, err)
				return
			}
		}
	}
	h.cookie(w, sessionCookie, "", -time.Second)
	h.cookie(w, csrfCookie, "", -time.Second)
	http.Redirect(w, r, "/login", 303)
}

type attempts struct {
	count int
	until time.Time
}
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]attempts
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{entries: map[string]attempts{}} }
func (l *loginLimiter) allow(remote string, now time.Time) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, v := range l.entries {
		if !v.until.After(now) {
			delete(l.entries, k)
		}
	}
	a, exists := l.entries[host]
	if !exists {
		if len(l.entries) >= 10000 {
			return false
		}
		a.until = now.Add(15 * time.Minute)
	}
	a.count++
	l.entries[host] = a
	return a.count <= 20
}
