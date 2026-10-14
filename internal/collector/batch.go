package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

type BatchProgress struct {
	ID        string `json:"id"`
	Running   bool   `json:"running"`
	Stopping  bool   `json:"stopping"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Query     string `json:"query"`
	Error     string `json:"error"`
}

func (s *Supervisor) Batch() BatchProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.batch
}

func (s *Supervisor) StopBatch() BatchProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batch.Running {
		s.batch.Stopping = true
	}
	return s.batch
}

func (s *Supervisor) SubmitBatch(ctx context.Context, subject, source string, queries []string, from, to string) (BatchProgress, error) {
	if len(queries) == 0 || len(queries) > 50 {
		return BatchProgress{}, fmt.Errorf("pilih 1–50 query")
	}
	if _, ok := s.Sources[source]; !ok {
		return BatchProgress{}, fmt.Errorf("unknown or disabled source")
	}
	if err := validateDateRange(from, to); err != nil {
		return BatchProgress{}, err
	}
	subjects, err := s.Store.Subjects(ctx)
	if err != nil {
		return BatchProgress{}, err
	}
	found := false
	batchLabel := subject
	for _, v := range subjects {
		if v.ID == subject {
			found = true
			batchLabel = v.FullName
		}
	}
	if !found {
		return BatchProgress{}, fmt.Errorf("subject not found")
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" || len(q) > 1000 {
			return BatchProgress{}, fmt.Errorf("query harus berisi 1–1000 byte")
		}
		key := strings.ToLower(q)
		if !seen[key] {
			unique = append(unique, q)
			seen[key] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.batch.Running || len(s.active) > 0 {
		return BatchProgress{}, fmt.Errorf("collector masih menjalankan antrean; tunggu selesai")
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		return BatchProgress{}, err
	}
	executionID := hex.EncodeToString(idBytes)
	if err = s.Store.CreateExecution(ctx, executionID, batchLabel, len(unique)); err != nil {
		return BatchProgress{}, err
	}
	s.batch = BatchProgress{ID: executionID, Running: true, Total: len(unique)}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { s.mu.Lock(); s.batch.Running = false; s.mu.Unlock() }()
		for _, q := range unique {
			s.mu.Lock()
			stop := s.batch.Stopping || s.closing
			if !stop {
				s.batch.Query = q
			}
			s.mu.Unlock()
			if stop {
				return
			}
			run, err := s.submitExecution(s.ctx, subject, source, q, from, to, executionID)
			if err == nil {
				run, err = s.Wait(s.ctx, run.ID)
			}
			s.mu.Lock()
			if err != nil {
				s.batch.Error = err.Error()
				s.mu.Unlock()
				return
			}
			if run.Status != "COMPLETED" {
				s.batch.Error = run.Status + ": " + run.ErrorMessage
				s.mu.Unlock()
				return
			}
			s.batch.Completed++
			s.mu.Unlock()
		}
	}()
	return s.batch, nil
}
