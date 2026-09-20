package providers

import (
	"context"
	"flight-search/internal/models"
)

type Provider interface {
	Name() string
	Search(ctx context.Context, params models.SearchParams) ([]models.Flight, error)
}
