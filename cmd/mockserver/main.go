package main

import (
	"flight-search/internal/dotenv"
	"flight-search/internal/mockapi"
	"log"
	"net/http"
	"os"
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
		{Path: "/garuda/search", File: "garuda_indonesia_search_response.json"},
		{Path: "/lionair/search", File: "lion_air_search_response.json"},
	}

	handler := mockapi.NewServer(fixturesDir, routes)

	log.Printf("mock airline API server listening on :%s (fixtures dir: %s)", port, fixturesDir)
	for _, r := range routes {
		log.Printf("  GET /%s%s -> %s", port, r.Path, r.File)
	}
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
