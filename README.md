# Учёт личных трат

Откройте http://localhost:8080. Остановка — Ctrl+C.

## Этапы и структура

- [Этап 1](https://github.com/svgamings19-ux/----22/tree/a08817283a407ca8bec8ebae1d12e48f700600f1) — самостоятельная версия простого HTTP-сервера. Для просмотра: `git switch --detach a08817283a407ca8bec8ebae1d12e48f700600f1`; вернуться: `git switch main`.
- `main` — этап 2: интерфейс на HTML-шаблонах поверх этапа 1.
- `cmd/expenses` — точка входа; корневой `main.go` сохраняет удобный запуск `go run .`.
- `internal/app` — запуск сервера.
- `internal/handlers` — маршруты, обработка форм и срез трат с блокировкой для параллельных запросов.
- `internal/models` — структура `Expense` с суммой в копейках, описанием и датой.
- `web/templates` — общий layout и шаблоны страниц; `web/static` — CSS.
- `docs` — отчёты и скриншоты.


## Маршруты

| Метод | Путь | Результат |
|---|---|---|
| GET | `/`, `/expenses` | Список трат |
| GET | `/expenses/new` | Форма добавления |
| POST | `/expenses/new` | Добавление и 303 на `/expenses` |
| GET | `/about` | Описание проекта |
| GET | `/ping` | 200 и `pong` |
| Другой метод | `/ping` | 405 и `Allow: GET` |


## Скриншот этапа 2

Тестовая запись добавлена через браузер

![Список трат](docs/screenshots/stage2-list.png)

