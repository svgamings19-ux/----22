package app

import (
	"log"
	"net/http"
	"time"

	"github.com/svgamings19-ux/----22/expenses/internal/handlers"
)

func Run() {
	h, err := handlers.New()
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: ":8080", Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Println("Сервер запущен: http://localhost:8080")
	log.Fatal(server.ListenAndServe())
}
