package collector

import (
	"context"
	"fmt"
)

func (s *Supervisor) ClearData(ctx context.Context, execution string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.batch.Running || len(s.active) > 0 || s.analysisBusy > 0 {
		return 0, fmt.Errorf("tunggu scraping dan analisis selesai sebelum menghapus data")
	}
	n, err := s.Store.ClearData(ctx, execution)
	if err == nil && (execution == "" || s.batch.ID == execution) {
		s.batch = BatchProgress{}
	}
	return n, err
}
