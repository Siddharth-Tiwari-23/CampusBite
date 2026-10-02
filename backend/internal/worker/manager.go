package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"campusbite/internal/config"
	"campusbite/internal/database"
)

// Manager coordinates the lifecycle of all background maintenance workers.
type Manager struct {
	db                *database.DB
	reservationWorker *ReservationWorker
	idempotencyWorker *IdempotencyWorker
	wg                sync.WaitGroup
	ctx               context.Context
	cancel            context.CancelFunc
}

// NewManager creates and initializes a new Worker Manager.
func NewManager(db *database.DB, cfg *config.Config) *Manager {
	resWorker := NewReservationWorker(db, cfg.ReservationWorkerInterval)
	idempWorker := NewIdempotencyWorker(db, cfg.IdempotencyCleanupInterval)

	return &Manager{
		db:                db,
		reservationWorker: resWorker,
		idempotencyWorker: idempWorker,
	}
}

// Start launches all configured background worker goroutines.
func (m *Manager) Start(parentCtx context.Context) {
	m.ctx, m.cancel = context.WithCancel(parentCtx)

	log.Println("[WorkerManager] Starting background workers...")

	m.wg.Add(1)
	go m.reservationWorker.Start(m.ctx, &m.wg)

	m.wg.Add(1)
	go m.idempotencyWorker.Start(m.ctx, &m.wg)

	log.Println("[WorkerManager] Background workers started successfully")
}

// Stop signals all background workers to stop and waits for their goroutines to terminate cleanly.
func (m *Manager) Stop(timeout time.Duration) error {
	if m.cancel != nil {
		m.cancel()
	}

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[WorkerManager] All background workers stopped cleanly")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("worker shutdown timed out after %v: %w", timeout, errors.New("timeout"))
	}
}

// GetReservationWorker returns the underlying ReservationWorker for direct invocation (useful in testing).
func (m *Manager) GetReservationWorker() *ReservationWorker {
	return m.reservationWorker
}

// GetIdempotencyWorker returns the underlying IdempotencyWorker for direct invocation (useful in testing).
func (m *Manager) GetIdempotencyWorker() *IdempotencyWorker {
	return m.idempotencyWorker
}
