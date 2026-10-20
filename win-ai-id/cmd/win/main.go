// Command win runs win.ai.id (and zh.win.ai.id) as a single binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"winaiid/internal/coach"
	"winaiid/internal/store"
	"winaiid/internal/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(k), 64); err == nil {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	st, err := store.Open(env("DB_PATH", "win.db"))
	if err != nil {
		log.Error("open db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	co := coach.New(coach.Config{
		BaseURL:         os.Getenv("LLM_BASE_URL"),
		APIKey:          os.Getenv("LLM_API_KEY"),
		Model:           env("LLM_MODEL", "claude-5-5-haiku"),
		BudgetUSD:       envFloat("LLM_BUDGET_USD", 5),
		InPricePerMTok:  envFloat("LLM_IN_PRICE_PER_MTOK", 0.80),
		OutPricePerMTok: envFloat("LLM_OUT_PRICE_PER_MTOK", 4.00),
	}, st)

	var allowed []string
	if v := os.Getenv("ALLOWED_EMAILS"); v != "" {
		allowed = strings.Split(v, ",")
	}
	baseURL := strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/")
	srv, err := web.New(web.Config{
		BaseURL:       baseURL,
		CookieDomain:  os.Getenv("COOKIE_DOMAIN"),
		SecureCookies: strings.HasPrefix(baseURL, "https://"),
		AllowedEmails: allowed,
	}, st, co, log)
	if err != nil {
		log.Error("init web", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              env("ADDR", ":8080"),
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("listening", "addr", httpSrv.Addr, "base_url", baseURL, "coach_ai", co.Enabled())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdown)
}
