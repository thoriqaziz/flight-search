package cache

import (
	"flight-search/internal/models"
	"strconv"
	"strings"
	"sync"
	"time"
)

type entry struct {
	flights   []models.Flight
	metadata  models.Metadata
	expiresAt time.Time
}

type Cache struct {
	mu    sync.RWMutex
	items map[string]entry
	ttl   time.Duration
}

func New(ttl time.Duration) *Cache {
	return &Cache{items: make(map[string]entry), ttl: ttl}
}

func Key(params models.SearchParams) string {
	return strings.ToUpper(params.Origin) + "|" +
		strings.ToUpper(params.Destination) + "|" +
		params.Date + "|" +
		strings.ToLower(params.CabinClass) + "|" +
		strconv.Itoa(params.Passengers)
}

func (c *Cache) Get(key string) ([]models.Flight, models.Metadata, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, models.Metadata{}, false
	}
	return cloneFlights(e.flights), e.metadata, true
}

func (c *Cache) Set(key string, flights []models.Flight, metadata models.Metadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = entry{
		flights:   cloneFlights(flights),
		metadata:  metadata,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *Cache) Purge() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.items {
		if now.After(e.expiresAt) {
			delete(c.items, k)
		}
	}
}

// StartJanitor runs Purge on the given interval until stop is closed.
func (c *Cache) StartJanitor(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.Purge()
			case <-stop:
				return
			}
		}
	}()
}

func cloneFlights(src []models.Flight) []models.Flight {
	cp := make([]models.Flight, len(src))
	copy(cp, src)
	return cp
}
