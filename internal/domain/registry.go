package domain

import (
	"fmt"
	"sync"
)

type LabRegistry interface {
	Register(lab Lab) error
	Get(labID string) (Lab, error)
}

type InMemoryRegistry struct {
	mu   sync.RWMutex
	labs map[string]Lab
}

func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{labs: make(map[string]Lab)}
}

func (r *InMemoryRegistry) Register(lab Lab) error {
	if lab.ID == "" {
		return fmt.Errorf("lab ID must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.labs[lab.ID]; exists {
		return fmt.Errorf("lab %q already registered", lab.ID)
	}
	r.labs[lab.ID] = lab
	return nil
}

func (r *InMemoryRegistry) Get(labID string) (Lab, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lab, ok := r.labs[labID]
	if !ok {
		return Lab{}, fmt.Errorf("lab %q not found", labID)
	}
	return lab, nil
}
