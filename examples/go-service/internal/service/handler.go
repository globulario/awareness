// Package service demonstrates idiomatic Go patterns that satisfy the
// awareness invariants for this project.
package service

import (
	"context"
	"fmt"
)

// CreateRequest is a request to create an item.
type CreateRequest struct {
	Key   string
	Value string
}

// CreateResponse is returned by Create.
type CreateResponse struct {
	Created bool
}

// Store is the dependency interface for the service.
type Store interface {
	// Upsert inserts or updates the item. Idempotent by key.
	Upsert(ctx context.Context, key, value string) (created bool, err error)
}

// Service handles business logic for the item resource.
type Service struct {
	store Store
}

// New constructs a Service with the required dependencies injected.
func New(store Store) *Service {
	return &Service{store: store}
}

// Create upserts an item. Satisfies idempotency.required: calling twice
// with the same key returns the same result without duplicating the record.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*CreateResponse, error) {
	if req.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	created, err := s.store.Upsert(ctx, req.Key, req.Value)
	if err != nil {
		return nil, fmt.Errorf("upsert: %w", err)
	}
	return &CreateResponse{Created: created}, nil
}
