package main

import (
	"context"
	"flight-search/internal/aggregator"
	"flight-search/internal/cache"
	"flight-search/internal/config"
	"flight-search/internal/dotenv"
	"flight-search/internal/handlers"
	"flight-search/internal/httpserver"
	"flight-search/internal/providers"
	"flight-search/internal/ranking"
	"flight-search/internal/ratelimit"
	"flight-search/internal/retry"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := dotenv.Load(".env"); err != nil {
		log.Printf("warning: failed to load .env: %v", err)
	}

	cfg := config.Load()

	httpClient := &http.Client{Timeout: cfg.ProviderHTTPTimeout}

	retryCfg := retry.Config{
		MaxAttempts: cfg.RetryMaxAttempts,
		BaseDelay:   cfg.RetryBaseDelay,
		MaxDelay:    cfg.RetryMaxDelay,
	}

	provs := []providers.Provider{
		providers.NewAirAsiaProvider(cfg.AirAsiaURL, httpClient, ratelimit.New(cfg.RateLimitRPS, cfg.RateLimitBurst), retryCfg),
		providers.NewBatikAirProvider(cfg.BatikAirURL, httpClient, ratelimit.New(cfg.RateLimitRPS, cfg.RateLimitBurst), retryCfg),
		providers.NewGarudaProvider(cfg.GarudaURL, httpClient, ratelimit.New(cfg.RateLimitRPS, cfg.RateLimitBurst), retryCfg),
		providers.NewLionAirProvider(cfg.LionAirURL, httpClient, ratelimit.New(cfg.RateLimitRPS, cfg.RateLimitBurst), retryCfg),
	}

	resultCache := cache.New(cfg.CacheTTL)
	stopJanitor := make(chan struct{})
	resultCache.StartJanitor(time.Minute, stopJanitor)
	defer close(stopJanitor)

	agg := aggregator.New(provs, resultCache, cfg.ProviderTimeout)

	searchHandler := &handlers.SearchHandler{Aggregator: agg, Weights: ranking.DefaultWeights()}
	router := httpserver.NewRouter(searchHandler)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("flight search API listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
