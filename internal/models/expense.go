package models

// Amount is stored in kopecks to avoid floating-point rounding in money.
type Expense struct {
	ID          int64
	UserID      int64
	Amount      int64
	Description string
	Date        string
	CategoryID  int64
	Category    string
}

type Category struct {
	ID   int64
	Name string
}
type User struct {
	ID           int64
	Email        string
	PasswordHash []byte
}
type Filter struct {
	CategoryID int64
	From       string
	To         string
}
type Stat struct {
	Label  string `json:"label"`
	Amount int64  `json:"amount"`
	Count  int64  `json:"count"`
}
