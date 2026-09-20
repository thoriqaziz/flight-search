package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port string

	AirAsiaURL  string
	BatikAirURL string
	GarudaURL   string
	LionAirURL  string

	ProviderTimeout     time.Duration
	ProviderHTTPTimeout time.Duration

	CacheTTL time.Duration

	RateLimitRPS   float64
	RateLimitBurst int

	RetryMaxAttempts int
	RetryBaseDelay   time.Duration
	RetryMaxDelay    time.Duration
}

func Load() Config {
	mockBase := getEnv("MOCK_API_BASE_URL", "http://localhost:8081")

	providerTimeoutMs := getEnvInt("PROVIDER_TIMEOUT_MS", 5000)
	providerHTTPTimeoutMs := getEnvInt("PROVIDER_HTTP_TIMEOUT_MS", 2000)
	cacheTTLSeconds := getEnvInt("CACHE_TTL_SECONDS", 180)

	retryMaxAttempts := getEnvInt("PROVIDER_RETRY_MAX_ATTEMPTS", 3)
	retryBaseDelayMs := getEnvInt("PROVIDER_RETRY_BASE_DELAY_MS", 200)
	retryMaxDelayMs := getEnvInt("PROVIDER_RETRY_MAX_DELAY_MS", 2000)

	return Config{
		Port: getEnv("PORT", "8080"),

		AirAsiaURL:  getEnv("AIRASIA_API_URL", mockBase+"/airasia/search"),
		BatikAirURL: getEnv("BATIKAIR_API_URL", mockBase+"/batikair/search"),
		GarudaURL:   getEnv("GARUDA_API_URL", mockBase+"/garuda/search"),
		LionAirURL:  getEnv("LIONAIR_API_URL", mockBase+"/lionair/search"),

		ProviderTimeout:     time.Duration(providerTimeoutMs) * time.Millisecond,
		ProviderHTTPTimeout: time.Duration(providerHTTPTimeoutMs) * time.Millisecond,

		CacheTTL: time.Duration(cacheTTLSeconds) * time.Second,

		RateLimitRPS:   getEnvFloat("PROVIDER_RATE_LIMIT_RPS", 5),
		RateLimitBurst: getEnvInt("PROVIDER_RATE_LIMIT_BURST", 5),

		RetryMaxAttempts: retryMaxAttempts,
		RetryBaseDelay:   time.Duration(retryBaseDelayMs) * time.Millisecond,
		RetryMaxDelay:    time.Duration(retryMaxDelayMs) * time.Millisecond,
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
