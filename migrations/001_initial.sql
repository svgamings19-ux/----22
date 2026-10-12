CREATE TABLE users (
 id INTEGER PRIMARY KEY,
 email TEXT NOT NULL UNIQUE COLLATE NOCASE,
 password_hash BLOB NOT NULL
);
CREATE TABLE categories (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL UNIQUE
);
INSERT INTO categories(name) VALUES ('Еда'), ('Транспорт'), ('Жильё'), ('Развлечения'), ('Здоровье'), ('Покупки'), ('Другое');
CREATE TABLE expenses (
 id INTEGER PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 amount INTEGER NOT NULL CHECK(amount > 0 AND amount <= 99999999999),
 description TEXT NOT NULL CHECK(length(description) BETWEEN 1 AND 200),
 date TEXT NOT NULL CHECK(length(date) = 10),
 category_id INTEGER NOT NULL REFERENCES categories(id)
);
CREATE INDEX expenses_user_date ON expenses(user_id, date);
CREATE INDEX expenses_user_category_date ON expenses(user_id, category_id, date);
CREATE TABLE sessions (
 session_id TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
