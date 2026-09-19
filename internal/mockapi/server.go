package mockapi

import (
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type FixtureRoute struct {
	Path string
	File string
}

func NewServer(fixturesDir string, routes []FixtureRoute) http.Handler {
	mux := http.NewServeMux()
	for _, r := range routes {
		route := r
		mux.HandleFunc(route.Path, func(w http.ResponseWriter, req *http.Request) {
			simulateLatency(req)

			if req.URL.Query().Get("fail") == "true" {
				http.Error(w, `{"error":"simulated provider outage"}`, http.StatusInternalServerError)
				return
			}
			if req.URL.Query().Get("malformed") == "true" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"status": "ok", "flights": [ this is not valid json`))
				return
			}

			data, err := os.ReadFile(filepath.Join(fixturesDir, route.File))
			if err != nil {
				http.Error(w, `{"error":"fixture not found"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
		})
	}
	return mux
}

func simulateLatency(req *http.Request) {
	latencyMs := 50 + rand.Intn(150)
	if d := req.URL.Query().Get("delay"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v >= 0 {
			latencyMs = v
		}
	}
	time.Sleep(time.Duration(latencyMs) * time.Millisecond)
}
