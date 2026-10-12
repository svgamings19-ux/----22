package handlers

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/svgamings19-ux/----22/expenses/internal/models"
)

func parseAmount(raw string) (int64, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(raw), ",", "."), ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, errors.New("Укажите положительную сумму с точностью до копеек.")
	}
	for _, part := range parts {
		if part == "" {
			return 0, errors.New("Неверный формат суммы.")
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, errors.New("Сумма должна быть числом.")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > 999999999 {
		return 0, errors.New("Максимальная сумма — 999999999,99 ₽.")
	}
	var fraction int64
	if len(parts) == 2 {
		if len(parts[1]) > 2 {
			return 0, errors.New("Допускается не больше двух знаков после запятой.")
		}
		fraction, _ = strconv.ParseInt(parts[1], 10, 64)
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	amount := whole*100 + fraction
	if amount <= 0 {
		return 0, errors.New("Сумма должна быть больше нуля.")
	}
	return amount, nil
}
func validDate(s string) bool {
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Year() >= 1 && t.Year() <= 9999 && t.Format("2006-01-02") == s
}
func parseFilter(q url.Values) (models.Filter, error) {
	f := models.Filter{From: q.Get("from"), To: q.Get("to")}
	if v := q.Get("category"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return f, errors.New("Выберите корректную категорию.")
		}
		f.CategoryID = n
	}
	if (f.From != "" && !validDate(f.From)) || (f.To != "" && !validDate(f.To)) {
		return f, errors.New("Укажите даты в формате ГГГГ-ММ-ДД.")
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		return f, errors.New("Начало периода не может быть позже окончания.")
	}
	return f, nil
}
