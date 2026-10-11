package storage

import "context"

type Media struct {
	ID               int64  `json:"id"`
	URL              string `json:"url"`
	SHA256           string `json:"sha256"`
	PerceptualHash   string `json:"perceptual_hash"`
	Distance         int    `json:"distance"`
	MatchesReference bool   `json:"matches_reference"`
	DuplicateOf      int64  `json:"duplicate_of"`
}

func (s *SQLite) Media(ctx context.Context, mentionID int64) ([]Media, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,url,COALESCE(sha256,''),COALESCE(perceptual_hash,''),COALESCE(distance,-1),matches_reference,COALESCE(duplicate_of,0) FROM mention_media WHERE mention_id=?", mentionID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Media{}
	for rows.Next() {
		var m Media
		if e = rows.Scan(&m.ID, &m.URL, &m.SHA256, &m.PerceptualHash, &m.Distance, &m.MatchesReference, &m.DuplicateOf); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
