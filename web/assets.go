package web

import "embed"

// Assets встраивает HTML и CSS в программу, чтобы запуск не зависел от рабочей папки.
//
//go:embed templates/*.html static/*.css
var Assets embed.FS
