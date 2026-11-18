package store

import (
	"context"
	"oj/internal/model"
)

// Repository is the domain persistence boundary. Authentication owns its own
// account/session transactions and does not expose password hashes through it.
type Repository interface {
	GetProblem(context.Context, int64) (*model.Problem, error)
	ListProblems(context.Context, ProblemFilter) ([]model.Problem, int, error)
	SaveProblem(context.Context, *model.Problem) error
	DeleteProblem(context.Context, int64) error
	ImportProblemsIfEmpty(context.Context, []model.Problem) error
	Tags(context.Context) ([]string, error)
	CreateSubmission(context.Context, *model.Submission) error
	GetSubmission(context.Context, int64) (*model.Submission, error)
	ListSubmissions(context.Context, SubmissionFilter) ([]model.Submission, int, error)
	PendingSubmissions(context.Context) ([]model.Submission, error)
	MarkJudging(context.Context, int64) error
	FinishSubmission(context.Context, *model.Submission) error
	PendingCount(context.Context, int64) (int, error)
	UserStats(context.Context, int64) (map[string]any, error)
}

var _ Repository = (*Store)(nil)
