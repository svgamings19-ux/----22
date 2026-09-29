package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/svgamings19-ux/----22/expenses/internal/models"
	"github.com/svgamings19-ux/----22/expenses/web"
)

type Handler struct {
	mu       sync.RWMutex
	expenses []models.Expense
	pages    map[string]*template.Template
}

type pageData struct {
	Title       string
	Expenses    []models.Expense
	Error       string
	Amount      string
	Description string
	Date        string
}

func New() (http.Handler, error) {
	h := &Handler{pages: make(map[string]*template.Template)}
	for _, page := range []string{"expenses", "new", "about"} {
		t, err := template.New("layout").Funcs(template.FuncMap{
			"money": func(n int64) string { return fmt.Sprintf("%d,%02d", n/100, n%100) },
			"date":  func(t time.Time) string { return t.Format("02.01.2006") },
		}).ParseFS(web.Assets, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return nil, err
		}
		h.pages[page] = t
	}
	static, err := fs.Sub(web.Assets, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("/", h.home)
	mux.HandleFunc("/expenses", h.list)
	mux.HandleFunc("/expenses/new", h.newExpense)
	mux.HandleFunc("/about", h.about)
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if !allow(w, r, http.MethodGet) {
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "pong")
	})
	return mux, nil
}

func allow(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	return false
}

func (h *Handler) render(w http.ResponseWriter, name string, status int, data pageData) {
	var buf bytes.Buffer
	if err := h.pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "Ошибка отображения страницы", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	h.list(w, r)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet) {
		return
	}
	h.mu.RLock()
	items := append([]models.Expense(nil), h.expenses...)
	h.mu.RUnlock()
	h.render(w, "expenses", http.StatusOK, pageData{Title: "Мои траты", Expenses: items})
}

func (h *Handler) about(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet) {
		return
	}
	h.render(w, "about", http.StatusOK, pageData{Title: "О проекте"})
}

func parseAmount(raw string) (int64, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(raw), ",", "."), ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("Укажите положительную сумму с точностью до копеек.")
	}
	for _, part := range parts {
		if part == "" {
			return 0, fmt.Errorf("Неверный формат суммы.")
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("Сумма должна быть числом.")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > 999999999 {
		return 0, fmt.Errorf("Сумма слишком велика (максимум 999999999,99).")
	}
	var fraction int64
	if len(parts) == 2 {
		if len(parts[1]) > 2 {
			return 0, fmt.Errorf("Допускается не больше двух знаков после запятой.")
		}
		fraction, _ = strconv.ParseInt(parts[1], 10, 64)
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	amount := whole*100 + fraction
	if amount <= 0 {
		return 0, fmt.Errorf("Сумма должна быть больше нуля.")
	}
	return amount, nil
}

func (h *Handler) newExpense(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	data := pageData{Title: "Добавить трату", Date: time.Now().Format("2006-01-02")}
	if r.Method == http.MethodGet {
		h.render(w, "new", http.StatusOK, data)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Не удалось прочитать форму", http.StatusBadRequest)
		return
	}
	data.Amount = strings.TrimSpace(r.PostFormValue("amount"))
	data.Description = strings.TrimSpace(r.PostFormValue("description"))
	data.Date = strings.TrimSpace(r.PostFormValue("date"))
	amount, err := parseAmount(data.Amount)
	date, dateErr := time.Parse("2006-01-02", data.Date)
	switch {
	case err != nil:
		data.Error = err.Error()
	case data.Description == "" || utf8.RuneCountInString(data.Description) > 200:
		data.Error = "Введите описание от 1 до 200 символов."
	case dateErr != nil || date.Year() < 1:
		data.Error = "Укажите существующую дату в формате ГГГГ-ММ-ДД."
	}
	if data.Error != "" {
		h.render(w, "new", http.StatusBadRequest, data)
		return
	}
	h.mu.Lock()
	h.expenses = append(h.expenses, models.Expense{Amount: amount, Description: data.Description, Date: date})
	h.mu.Unlock()
	// Post/Redirect/Get: обновление страницы повторяет GET, а не POST.
	http.Redirect(w, r, "/expenses", http.StatusSeeOther)
}
