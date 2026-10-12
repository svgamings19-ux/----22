package handlers

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/svgamings19-ux/----22/expenses/internal/config"
	"github.com/svgamings19-ux/----22/expenses/internal/models"
	"github.com/svgamings19-ux/----22/expenses/internal/repository"
	"github.com/svgamings19-ux/----22/expenses/web"
)

type Handler struct {
	repo      *repository.ExpenseRepository
	cfg       config.Config
	logger    *log.Logger
	pages     map[string]*template.Template
	limiter   *loginLimiter
	dummyHash []byte
}
type pageData struct {
	Title       string
	Notice      string
	User        models.User
	CSRF        string
	Error       string
	Errors      map[string]string
	Expenses    []models.Expense
	Categories  []models.Category
	Expense     models.Expense
	Filter      models.Filter
	Amount      string
	Description string
	Date        string
	CategoryID  int64
	Email       string
	Mode        string
	Action      string
	Total       int64
	Year        int
	Query       string
	Status      int
}

func money(n int64) string { return fmt.Sprintf("%d,%02d", n/100, n%100) }
func New(repo *repository.ExpenseRepository, cfg config.Config, logger *log.Logger) (http.Handler, error) {
	h := &Handler{repo: repo, cfg: cfg, logger: logger, pages: make(map[string]*template.Template), limiter: newLoginLimiter()}
	if err := h.prepareAuth(); err != nil {
		return nil, err
	}
	for _, name := range []string{"expenses", "expense", "delete", "auth", "stats", "about", "404", "500", "error"} {
		t, err := template.New("layout").Funcs(template.FuncMap{"money": money, "date": func(s string) string {
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				return s
			}
			return t.Format("02.01.2006")
		}}).ParseFS(web.Assets, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		h.pages[name] = t
	}
	static, err := fs.Sub(web.Assets, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /about", func(w http.ResponseWriter, r *http.Request) {
		h.render(w, r, "about", 200, pageData{Title: "О приложении"})
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			h.fail(w, r, 405, "Метод не поддерживается.")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "pong")
	})
	mux.HandleFunc("GET /register", h.authForm)
	mux.HandleFunc("POST /register", h.register)
	mux.HandleFunc("GET /login", h.authForm)
	mux.HandleFunc("POST /login", h.login)
	mux.Handle("POST /logout", h.requireAuth(http.HandlerFunc(h.logout)))
	mux.Handle("GET /{$}", h.requireAuth(http.HandlerFunc(h.list)))
	mux.Handle("GET /expenses", h.requireAuth(http.HandlerFunc(h.list)))
	mux.Handle("GET /expenses/new", h.requireAuth(http.HandlerFunc(h.expenseForm)))
	mux.Handle("POST /expenses/new", h.requireAuth(http.HandlerFunc(h.saveExpense)))
	mux.Handle("GET /expenses/{id}/edit", h.requireAuth(http.HandlerFunc(h.expenseForm)))
	mux.Handle("POST /expenses/{id}/edit", h.requireAuth(http.HandlerFunc(h.saveExpense)))
	mux.Handle("GET /expenses/{id}/delete", h.requireAuth(http.HandlerFunc(h.deleteForm)))
	mux.Handle("POST /expenses/{id}/delete", h.requireAuth(http.HandlerFunc(h.deleteExpense)))
	mux.Handle("GET /stats", h.requireAuth(http.HandlerFunc(h.statsPage)))
	mux.Handle("GET /api/stats/by-category", h.requireAuth(http.HandlerFunc(h.byCategory)))
	mux.Handle("GET /api/stats/by-month", h.requireAuth(http.HandlerFunc(h.byMonth)))
	mux.Handle("GET /expenses/export", h.requireAuth(http.HandlerFunc(h.export)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { h.fail(w, r, 404, "") })
	return h.logging(h.recoverer(h.security(h.session(h.csrf(mux))))), nil
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, status int, d pageData) {
	d.User = currentUser(r)
	d.CSRF = csrfValue(r)
	var b bytes.Buffer
	if err := h.pages[name].ExecuteTemplate(&b, "layout", d); err != nil {
		h.logger.Printf("template %s: %v", name, err)
		http.Error(w, "Ошибка отображения страницы", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, status int, message string) {
	if message == "" {
		switch status {
		case 404:
			message = "Страница или запись не найдена."
		case 500:
			message = "Не удалось выполнить запрос. Попробуйте позже."
		default:
			message = http.StatusText(status)
		}
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		return
	}
	page := "error"
	if status == 404 {
		page = "404"
	}
	if status == 500 {
		page = "500"
	}
	h.render(w, r, page, status, pageData{Title: fmt.Sprintf("Ошибка %d", status), Error: message, Status: status})
}
func (h *Handler) dbError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		h.fail(w, r, 404, "")
		return
	}
	h.logger.Printf("request failed: %v", err)
	h.fail(w, r, 500, "")
}
func (h *Handler) categories(w http.ResponseWriter, r *http.Request) ([]models.Category, bool) {
	cats, err := h.repo.Categories(r.Context())
	if err != nil {
		h.dbError(w, r, err)
		return nil, false
	}
	return cats, true
}
func hasCategory(cats []models.Category, id int64) bool {
	for _, c := range cats {
		if c.ID == id {
			return true
		}
	}
	return false
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	cats, ok := h.categories(w, r)
	if !ok {
		return
	}
	f, err := parseFilter(r.URL.Query())
	if err == nil && f.CategoryID != 0 && !hasCategory(cats, f.CategoryID) {
		err = errors.New("Категория не найдена.")
	}
	d := pageData{Title: "Мои траты", Categories: cats, Filter: f, Query: r.URL.Query().Encode()}
	if err != nil {
		d.Error = err.Error()
		h.render(w, r, "expenses", 400, d)
		return
	}
	d.Expenses, err = h.repo.GetAll(r.Context(), currentUser(r).ID, f)
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	for _, e := range d.Expenses {
		d.Total += e.Amount
	}
	h.render(w, r, "expenses", 200, d)
}
func (h *Handler) findExpense(w http.ResponseWriter, r *http.Request) (models.Expense, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		h.fail(w, r, 404, "")
		return models.Expense{}, false
	}
	e, err := h.repo.GetByID(r.Context(), currentUser(r).ID, id)
	if err != nil {
		h.dbError(w, r, err)
		return e, false
	}
	return e, true
}
func (h *Handler) expenseForm(w http.ResponseWriter, r *http.Request) {
	cats, ok := h.categories(w, r)
	if !ok {
		return
	}
	d := pageData{Title: "Новая трата", Date: time.Now().Format("2006-01-02"), Categories: cats, Action: "/expenses/new"}
	if r.PathValue("id") != "" {
		e, ok := h.findExpense(w, r)
		if !ok {
			return
		}
		d.Title = "Редактировать трату"
		d.Expense = e
		d.Amount = money(e.Amount)
		d.Description = e.Description
		d.Date = e.Date
		d.CategoryID = e.CategoryID
		d.Action = r.URL.Path
	}
	h.render(w, r, "expense", 200, d)
}
func (h *Handler) saveExpense(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.PathValue("id") != "" {
		e, ok := h.findExpense(w, r)
		if !ok {
			return
		}
		id = e.ID
	}
	cats, ok := h.categories(w, r)
	if !ok {
		return
	}
	d := pageData{Title: "Новая трата", Categories: cats, Action: r.URL.Path, Amount: strings.TrimSpace(r.PostFormValue("amount")), Description: strings.TrimSpace(r.PostFormValue("description")), Date: r.PostFormValue("date"), Errors: map[string]string{}}
	if id != 0 {
		d.Title = "Редактировать трату"
	}
	amount, err := parseAmount(d.Amount)
	if err != nil {
		d.Errors["amount"] = err.Error()
	}
	if !utf8.ValidString(d.Description) || utf8.RuneCountInString(d.Description) < 1 || utf8.RuneCountInString(d.Description) > 200 {
		d.Errors["description"] = "Введите описание от 1 до 200 символов."
	}
	if !validDate(d.Date) {
		d.Errors["date"] = "Укажите существующую дату в формате ГГГГ-ММ-ДД."
	}
	d.CategoryID, err = strconv.ParseInt(r.PostFormValue("category_id"), 10, 64)
	if err != nil || !hasCategory(cats, d.CategoryID) {
		d.Errors["category"] = "Выберите категорию из списка."
	}
	if len(d.Errors) > 0 {
		d.Error = "Проверьте отмеченные поля."
		h.render(w, r, "expense", 400, d)
		return
	}
	e := models.Expense{Amount: amount, Description: d.Description, Date: d.Date, CategoryID: d.CategoryID}
	if id == 0 {
		_, err = h.repo.Create(r.Context(), currentUser(r).ID, e)
	} else {
		err = h.repo.Update(r.Context(), currentUser(r).ID, id, e)
	}
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	http.Redirect(w, r, "/expenses", http.StatusSeeOther)
}
func (h *Handler) deleteForm(w http.ResponseWriter, r *http.Request) {
	e, ok := h.findExpense(w, r)
	if !ok {
		return
	}
	h.render(w, r, "delete", 200, pageData{Title: "Удалить трату", Expense: e, Action: r.URL.Path})
}
func (h *Handler) deleteExpense(w http.ResponseWriter, r *http.Request) {
	e, ok := h.findExpense(w, r)
	if !ok {
		return
	}
	if err := h.repo.Delete(r.Context(), currentUser(r).ID, e.ID); err != nil {
		h.dbError(w, r, err)
		return
	}
	http.Redirect(w, r, "/expenses", 303)
}
func (h *Handler) statsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "stats", 200, pageData{Title: "Статистика", Year: time.Now().Year()})
}
func (h *Handler) byCategory(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r.URL.Query())
	if err != nil {
		h.fail(w, r, 400, err.Error())
		return
	}
	if f.CategoryID != 0 {
		cats, ok := h.categories(w, r)
		if !ok {
			return
		}
		if !hasCategory(cats, f.CategoryID) {
			h.fail(w, r, 400, "Категория не найдена.")
			return
		}
	}
	data, err := h.repo.ByCategory(r.Context(), currentUser(r).ID, f)
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	h.json(w, data)
}
func (h *Handler) byMonth(w http.ResponseWriter, r *http.Request) {
	year := time.Now().Year()
	if v := r.URL.Query().Get("year"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 9999 {
			h.fail(w, r, 400, "Год должен быть от 1 до 9999.")
			return
		}
		year = n
	}
	data, err := h.repo.ByMonth(r.Context(), currentUser(r).ID, year)
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	h.json(w, data)
}
func (h *Handler) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// Prevent spreadsheet software from interpreting user text as a formula.
func csvText(s string) string {
	trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + s
	}
	if strings.ContainsAny(s, "\t\r\n") {
		return "'" + s
	}
	return s
}
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r.URL.Query())
	if err != nil {
		h.fail(w, r, 400, err.Error())
		return
	}
	if f.CategoryID != 0 {
		cats, ok := h.categories(w, r)
		if !ok {
			return
		}
		if !hasCategory(cats, f.CategoryID) {
			h.fail(w, r, 400, "Категория не найдена.")
			return
		}
	}
	items, err := h.repo.GetAll(r.Context(), currentUser(r).ID, f)
	if err != nil {
		h.dbError(w, r, err)
		return
	}
	var b bytes.Buffer
	b.WriteString("\ufeff")
	cw := csv.NewWriter(&b)
	cw.Comma = ';'
	cw.UseCRLF = true
	_ = cw.Write([]string{"Дата", "Категория", "Описание", "Сумма (RUB)"})
	for _, e := range items {
		_ = cw.Write([]string{e.Date, csvText(e.Category), csvText(e.Description), money(e.Amount)})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		h.dbError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="expenses-`+time.Now().Format("2006-01-02")+`.csv"`)
	_, _ = w.Write(b.Bytes())
}
