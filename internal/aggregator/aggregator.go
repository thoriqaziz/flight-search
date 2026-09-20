package aggregator

import (
	"context"
	"flight-search/internal/cache"
	"flight-search/internal/models"
	"flight-search/internal/providers"
	"sync"
	"time"
)

type Aggregator struct {
	providers       []providers.Provider
	cache           *cache.Cache
	providerTimeout time.Duration
}

func New(provs []providers.Provider, c *cache.Cache, providerTimeout time.Duration) *Aggregator {
	return &Aggregator{providers: provs, cache: c, providerTimeout: providerTimeout}
}

type providerResult struct {
	name    string
	flights []models.Flight
	err     error
}

func (a *Aggregator) Search(ctx context.Context, params models.SearchParams, bypassCache bool) ([]models.Flight, models.Metadata, error) {
	start := time.Now()

	key := cache.Key(params)
	if !bypassCache {
		if flights, meta, ok := a.cache.Get(key); ok {
			meta.CacheHit = true
			meta.SearchTimeMs = time.Since(start).Milliseconds()
			return flights, meta, nil
		}
	}

	results := make([]providerResult, len(a.providers))
	var wg sync.WaitGroup
	for i, p := range a.providers {
		wg.Add(1)
		go func(i int, p providers.Provider) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, a.providerTimeout)
			defer cancel()
			flights, err := p.Search(pctx, params)
			results[i] = providerResult{name: p.Name(), flights: flights, err: err}
		}(i, p)
	}
	wg.Wait()

	var all []models.Flight
	var errs []models.ProviderError
	succeeded := 0
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, models.ProviderError{Provider: r.name, Error: r.err.Error()})
			continue
		}
		succeeded++
		all = append(all, r.flights...)
	}

	matched := filterByCriteria(all, params)
	merged := mergeDuplicates(matched)

	meta := models.Metadata{
		TotalResults:       len(merged),
		ProvidersQueried:   len(a.providers),
		ProvidersSucceeded: succeeded,
		ProvidersFailed:    len(a.providers) - succeeded,
		ProviderErrors:     errs,
		SearchTimeMs:       time.Since(start).Milliseconds(),
		CacheHit:           false,
	}

	a.cache.Set(key, merged, meta)
	return merged, meta, nil
}
