package model

import "time"

type Case struct {
	ID        int64     `json:"id"`
	MentionID int64     `json:"mention_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type Audit struct {
	ID        int64  `json:"id"`
	Entity    string `json:"entity"`
	EntityID  string `json:"entity_id"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"created_at"`
}

func CaseTransition(from, to string) bool {
	return map[string]map[string]bool{
		"OPEN":               {"UNDER_REVIEW": true, "WAITING_APPROVAL": true, "CLOSED": true},
		"UNDER_REVIEW":       {"WAITING_APPROVAL": true, "CLOSED": true},
		"WAITING_APPROVAL":   {"APPROVED": true, "REJECTED": true},
		"APPROVED":           {"SUBMITTED_MANUALLY": true, "CLOSED": true},
		"REJECTED":           {"UNDER_REVIEW": true, "CLOSED": true},
		"SUBMITTED_MANUALLY": {"PLATFORM_REVIEWING": true, "REMOVED": true, "NOT_REMOVED": true, "CLOSED": true},
		"PLATFORM_REVIEWING": {"REMOVED": true, "NOT_REMOVED": true, "CLOSED": true},
		"REMOVED":            {"CLOSED": true}, "NOT_REMOVED": {"CLOSED": true},
	}[from][to]
}
