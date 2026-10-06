package main

import (
	"flight-search/internal/dotenv"
	"flight-search/internal/mockapi"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := dotenv.Load(".env"); err != nil {
		log.Printf("warning: failed to load .env: %v", err)
	}

	port := getEnv("MOCK_SERVER_PORT", "8081")
	fixturesDir := getEnv("FIXTURES_DIR", "input")

	routes := []mockapi.FixtureRoute{
		{Path: "/airasia/search", File: "airasia_search_response.json"},
		{Path: "/batikair/search", File: "batik_air_search_response.json"},
		garudaRoute(),
		{Path: "/lionair/search", File: "lion_air_search_response.json"},
	}

	handler := mockapi.NewServer(fixturesDir, routes)

	log.Printf("mock airline API server listening on :%s (fixtures dir: %s)", port, fixturesDir)
	for _, r := range routes {
		method := r.Method
		if method == "" {
			method = http.MethodGet
		}
		log.Printf("  %s :%s%s -> %s", method, port, r.Path, r.File)
	}
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

// garudaRoute protects the Garuda mock with OAuth2 client credentials when
// GARUDA_CLIENT_ID/GARUDA_CLIENT_SECRET are set (the same vars cmd/server
// reads, so one .env configures both sides); otherwise it stays open.
func garudaRoute() mockapi.FixtureRoute {
	route := mockapi.FixtureRoute{Path: "/garuda/search", File: "garuda_indonesia_search_response.json", Method: http.MethodPost}
	id, secret := os.Getenv("GARUDA_CLIENT_ID"), os.Getenv("GARUDA_CLIENT_SECRET")
	if id != "" {
		route.Auth = mockapi.NewOAuthIssuer(id, secret, time.Hour)
		route.TokenPath = "/garuda/oauth/token"
		log.Printf("garuda mock requires OAuth2 client credentials (token endpoint POST %s)", route.TokenPath)
	}
	return route
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
