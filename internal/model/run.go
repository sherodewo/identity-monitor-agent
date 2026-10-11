package model

import "time"

type SearchTask struct {
	ID         string `json:"id"`
	SubjectID  string `json:"subject_id"`
	Source     string `json:"source"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	DateFrom   string `json:"date_from,omitempty"`
	DateTo     string `json:"date_to,omitempty"`
}
type SessionStatus string

const (
	SessionReady   SessionStatus = "READY"
	SessionUnknown SessionStatus = "UNKNOWN"
	SessionHuman   SessionStatus = "HUMAN_ACTION_REQUIRED"
)

type Run struct {
	ID              string     `json:"run_id"`
	SubjectID       string     `json:"subject_id"`
	Source          string     `json:"source"`
	Query           string     `json:"query"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	Status          string     `json:"status"`
	TotalDiscovered int        `json:"total_discovered"`
	TotalStored     int        `json:"total_stored"`
	TotalDuplicate  int        `json:"total_duplicate"`
	TotalFailed     int        `json:"total_failed"`
	ErrorMessage    string     `json:"error_message"`
	DateFrom        string     `json:"date_from,omitempty"`
	DateTo          string     `json:"date_to,omitempty"`
}

func RunTransition(from, to string) bool {
	return map[string]map[string]bool{
		"PENDING": {"RUNNING": true, "CANCELLED": true}, "RUNNING": {"COMPLETED": true, "FAILED": true, "PAUSED": true, "HUMAN_ACTION_REQUIRED": true, "CANCELLED": true},
		"FAILED": {"PENDING": true}, "PAUSED": {"PENDING": true}, "HUMAN_ACTION_REQUIRED": {"PENDING": true},
	}[from][to]
}
