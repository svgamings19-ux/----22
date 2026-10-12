package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/svgamings19-ux/----22/expenses/internal/config"
	"github.com/svgamings19-ux/----22/expenses/internal/handlers"
	"github.com/svgamings19-ux/----22/expenses/internal/repository"
)

func Run() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	repo, err := repository.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	handler, err := handlers.New(repo, cfg, log.Default())
	if err != nil {
		return err
	}
	server := &http.Server{Addr: ":" + cfg.Port, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	done := make(chan error, 1)
	go func() {
		log.Printf("Учёт личных трат: http://localhost:%s", cfg.Port)
		done <- server.ListenAndServe()
	}()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		<-done
	}
	return nil
}
