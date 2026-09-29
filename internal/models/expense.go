package models

import "time"

// Expense хранит сумму в копейках: целые числа исключают ошибки округления денег.
type Expense struct {
	Amount      int64
	Description string
	Date        time.Time
}
