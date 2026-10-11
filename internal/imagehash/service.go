package imagehash

import (
	"context"
	"fmt"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/storage"
	"io"
	"math/bits"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	Store     *storage.SQLite
	Threshold int
}

func (s *Service) References(ctx context.Context, subject model.Subject) error {
	for _, path := range subject.ReferenceImages {
		f, e := os.Open(path)
		if e != nil {
			return fmt.Errorf("reference image %q: %w", path, e)
		}
		data, e := io.ReadAll(io.LimitReader(f, 10<<20+1))
		_ = f.Close()
		if e != nil {
			return e
		}
		sha, h, e := Hash(data)
		if e != nil {
			return e
		}
		if _, e = s.Store.DB.ExecContext(ctx, "INSERT INTO subject_images(subject_id,path,sha256,perceptual_hash) VALUES (?,?,?,?) ON CONFLICT(subject_id,path) DO UPDATE SET sha256=excluded.sha256,perceptual_hash=excluded.perceptual_hash", subject.ID, path, sha, fmt.Sprintf("%016x", h)); e != nil {
			return e
		}
	}
	return nil
}

// Only explicitly allowlisted public HTTPS images are fetched; never cookies or arbitrary files.
func (s *Service) Candidates(ctx context.Context, id int64, subject model.Subject, urls, domains []string) error {
	if len(subject.ReferenceImages) == 0 {
		return nil
	}
	policy := guardrail.Policy{Domains: domains}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("redirect limit")
		}
		return policy.URL(r.URL.String())
	}}
	for i, url := range urls {
		if i >= 5 {
			break
		}
		if policy.URL(url) != nil {
			continue
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if e != nil {
			return e
		}
		resp, e := client.Do(req)
		if e != nil {
			continue
		}
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			_ = resp.Body.Close()
			continue
		}
		data, e := io.ReadAll(io.LimitReader(resp.Body, 10<<20+1))
		_ = resp.Body.Close()
		if e != nil {
			continue
		}
		sha, h, e := Hash(data)
		if e != nil {
			continue
		}
		best := 65
		matched := false
		for _, path := range subject.ReferenceImages {
			ref, e := os.ReadFile(path)
			if e != nil {
				continue
			}
			result, e := (Matcher{Threshold: s.Threshold}).Compare(ctx, ImageInput{ref}, ImageInput{data})
			if e != nil {
				continue
			}
			if result.Distance < best {
				best = result.Distance
			}
			matched = matched || result.Similar
		}
		if _, e = s.Store.DB.ExecContext(ctx, "UPDATE mention_media SET sha256=?,perceptual_hash=?,distance=?,matches_reference=? WHERE mention_id=? AND url=?", sha, fmt.Sprintf("%016x", h), best, matched, id, url); e != nil {
			return e
		}
		rows, e := s.Store.DB.QueryContext(ctx, "SELECT mm.id,mm.sha256,mm.perceptual_hash FROM mention_media mm JOIN mentions m ON m.id=mm.mention_id WHERE m.subject_id=? AND mm.mention_id<>? AND mm.perceptual_hash IS NOT NULL ORDER BY mm.id", subject.ID, id)
		if e != nil {
			return e
		}
		var duplicateID int64
		for rows.Next() {
			var mid int64
			var oldSHA, oldHash string
			if e = rows.Scan(&mid, &oldSHA, &oldHash); e != nil {
				break
			}
			old, parseErr := strconv.ParseUint(oldHash, 16, 64)
			if oldSHA == sha || (parseErr == nil && bits.OnesCount64(old^h) <= s.Threshold) {
				duplicateID = mid
				break
			}
		}
		rowErr := rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		if rowErr != nil {
			return rowErr
		}
		if duplicateID != 0 {
			if _, e = s.Store.DB.ExecContext(ctx, "UPDATE mention_media SET duplicate_of=? WHERE mention_id=? AND url=?", duplicateID, id, url); e != nil {
				return e
			}
		}
	}
	return nil
}
