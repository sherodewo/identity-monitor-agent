package storage

import (
	"context"
	"identity-monitor-agent/internal/model"
)

type Repository interface {
	UpsertSubject(context.Context, model.Subject) error
	Subjects(context.Context) ([]model.Subject, error)
	SaveMention(context.Context, model.Mention, int) (int64, bool, error)
	Mentions(context.Context, model.MentionFilter) ([]model.Mention, int, error)
	Mention(context.Context, int64) (model.Mention, error)
	CreateRun(context.Context, model.Run) error
	UpdateRun(context.Context, model.Run) error
	Runs(context.Context) ([]model.Run, error)
	CreateCase(context.Context, int64, string) (model.Case, error)
	Cases(context.Context) ([]model.Case, error)
	TransitionCase(context.Context, int64, string, string) error
}
