package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed schema.sql
var schema string

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func connect() (*sql.DB, error) {
	if os.Getenv("POSTGRES_PASSWORD") == "" {
		return nil, errors.New("POSTGRES_PASSWORD is required")
	}
	u := &url.URL{Scheme: "postgres", User: url.UserPassword(env("POSTGRES_USER", "validator"), os.Getenv("POSTGRES_PASSWORD")), Host: net.JoinHostPort(env("POSTGRES_HOST", "localhost"), env("POSTGRES_PORT", "5432")), Path: "/" + env("POSTGRES_DB", "project-sem-1")}
	q := url.Values{"sslmode": {env("POSTGRES_SSLMODE", "disable")}, "connect_timeout": {"5"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	db, err := connect()
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err = db.ExecContext(ctx, schema)
	cancel()
	if err != nil {
		return fmt.Errorf("prepare schema: %w", err)
	}
	if len(os.Args) > 1 && os.Args[1] == "--prepare" {
		return nil
	}
	api := &API{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0/prices", api.prices)
	mux.HandleFunc("/healthz", api.health)
	server := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	stop, cancelSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelSignal()
	errs := make(chan error, 1)
	go func() { log.Printf("listening on %s", server.Addr); errs <- server.ListenAndServe() }()
	select {
	case err = <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stop.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}
