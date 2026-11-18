package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"oj/internal/model"
)

const submissionColumns = `s.id,s.problem_id,s.user_id,CASE WHEN p.id IS NULL THEN s.problem_title || '（题目已删除）' ELSE p.title END,s.language,s.code,s.status,s.time_us,s.memory_kb,s.passed,s.total,s.message,s.case_results,s.created_at,s.judged_at`

func (s *Store) CreateSubmission(ctx context.Context, sub *model.Submission) error {
	if sub == nil {
		return errors.New("nil submission")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM submissions WHERE user_id=? AND status IN ('pending','judging')", sub.UserID).Scan(&count); err != nil {
		return err
	}
	if count >= 3 {
		return ErrTooManyPending
	}
	var title string
	if err = tx.QueryRowContext(ctx, "SELECT title FROM problems WHERE id=?", sub.ProblemID).Scan(&title); err != nil {
		return err
	}
	created := now()
	res, err := tx.ExecContext(ctx, "INSERT INTO submissions(problem_id,problem_title,user_id,language,code,status,created_at) VALUES(?,?,?,?,?,'pending',?)", sub.ProblemID, title, sub.UserID, sub.Language, sub.Code, created)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE problems SET submit_count=submit_count+1 WHERE id=?", sub.ProblemID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*sub = model.Submission{ID: id, ProblemID: sub.ProblemID, UserID: sub.UserID, Language: sub.Language, Code: sub.Code, ProblemTitle: title, Status: "pending", CreatedAt: created, CaseResults: []model.CaseResult{}}
	return nil
}

func scanSubmission(row scanner) (*model.Submission, error) {
	sub := new(model.Submission)
	var raw string
	err := row.Scan(&sub.ID, &sub.ProblemID, &sub.UserID, &sub.ProblemTitle, &sub.Language, &sub.Code, &sub.Status, &sub.TimeUs, &sub.MemoryKB, &sub.Passed, &sub.Total, &sub.Message, &raw, &sub.CreatedAt, &sub.JudgedAt)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &sub.CaseResults); err != nil {
		return nil, err
	}
	sub.TimeMS = ceilMS(sub.TimeUs)
	sub.CaseResults = safeCases(sub.CaseResults)
	return sub, nil
}
func (s *Store) GetSubmission(ctx context.Context, id int64) (*model.Submission, error) {
	return scanSubmission(s.DB.QueryRowContext(ctx, "SELECT "+submissionColumns+" FROM submissions s LEFT JOIN problems p ON p.id=s.problem_id WHERE s.id=?", id))
}

func (s *Store) ListSubmissions(ctx context.Context, f SubmissionFilter) ([]model.Submission, int, error) {
	where, args := " WHERE 1=1", []any{}
	if f.UserID != 0 {
		where += " AND s.user_id=?"
		args = append(args, f.UserID)
	}
	if f.ProblemID != 0 {
		where += " AND s.problem_id=?"
		args = append(args, f.ProblemID)
	}
	if f.Status != "" {
		where += " AND s.status=?"
		args = append(args, f.Status)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM submissions s"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := paging(f.Page, f.PageSize)
	args = append(args, limit, offset)
	items, err := querySubmissions(ctx, tx, where+" ORDER BY s.id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func querySubmissions(ctx context.Context, q querier, suffix string, args ...any) ([]model.Submission, error) {
	rows, err := q.QueryContext(ctx, "SELECT "+submissionColumns+" FROM submissions s LEFT JOIN problems p ON p.id=s.problem_id"+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Submission{}
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *sub)
	}
	return items, rows.Err()
}
func (s *Store) PendingSubmissions(ctx context.Context) ([]model.Submission, error) {
	return querySubmissions(ctx, s.DB, " WHERE s.status IN ('pending','judging') ORDER BY s.id")
}
func (s *Store) PendingCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM submissions WHERE user_id=? AND status IN ('pending','judging')", userID).Scan(&n)
	return n, err
}
func (s *Store) MarkJudging(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, "UPDATE submissions SET status='judging' WHERE id=? AND status IN ('pending','judging')", id)
	if err = changed(res, err); errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidTransition
	}
	return err
}

func (s *Store) FinishSubmission(ctx context.Context, sub *model.Submission) error {
	if sub == nil || !terminal(sub.Status) {
		return ErrInvalidTransition
	}
	if sub.TimeUs < 0 || sub.MemoryKB < 0 || sub.Passed < 0 || sub.Total < sub.Passed {
		return errors.New("invalid submission metrics")
	}
	cases := safeCases(sub.CaseResults)
	raw, err := json.Marshal(cases)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID, problemID int64
	var previous string
	if err = tx.QueryRowContext(ctx, "SELECT user_id,problem_id,status FROM submissions WHERE id=?", sub.ID).Scan(&userID, &problemID, &previous); err != nil {
		return err
	}
	// Repeated delivery must not double-count an accepted result.
	if terminal(previous) {
		if previous == sub.Status {
			return tx.Commit()
		}
		return ErrInvalidTransition
	}
	judged := now()
	if _, err = tx.ExecContext(ctx, "UPDATE submissions SET status=?,time_us=?,memory_kb=?,passed=?,total=?,message=?,case_results=?,judged_at=? WHERE id=?", sub.Status, sub.TimeUs, sub.MemoryKB, sub.Passed, sub.Total, sub.Message, string(raw), judged, sub.ID); err != nil {
		return err
	}
	status := "attempted"
	if sub.Status == "accepted" {
		status = "solved"
		if _, err = tx.ExecContext(ctx, "UPDATE problems SET accepted_count=accepted_count+1 WHERE id=?", problemID); err != nil {
			return err
		}
	}
	// Deleted problems have no status row, but their in-flight submissions still
	// finish. A later failed submission can never downgrade a solved problem.
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_problem_status(user_id,problem_id,status,updated_at)
	 SELECT ?,id,?,? FROM problems WHERE id=?
	 ON CONFLICT(user_id,problem_id) DO UPDATE SET status=CASE WHEN user_problem_status.status='solved' THEN 'solved' ELSE excluded.status END,updated_at=excluded.updated_at`, userID, status, judged, problemID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	sub.UserID = userID
	sub.ProblemID = problemID
	sub.JudgedAt = judged
	sub.TimeMS = ceilMS(sub.TimeUs)
	sub.CaseResults = cases
	return nil
}

func terminal(status string) bool {
	switch status {
	case "accepted", "wrong_answer", "time_limit_exceeded", "memory_limit_exceeded", "runtime_error", "compile_error", "output_limit_exceeded", "system_error":
		return true
	}
	return false
}
func ceilMS(us int64) int64 {
	if us <= 0 {
		return 0
	}
	return us/1000 + int64(boolInt(us%1000 != 0))
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func safeCases(input []model.CaseResult) []model.CaseResult {
	output := make([]model.CaseResult, len(input))
	copy(output, input)
	for i := range output {
		c := &output[i]
		c.TimeMS = ceilMS(c.TimeUs)
		if !c.IsSample {
			c.Input = ""
			c.Expected = ""
			c.Actual = ""
		} else {
			c.Input = truncate(c.Input, 2048)
			c.Expected = truncate(c.Expected, 2048)
			c.Actual = truncate(c.Actual, 2048)
		}
	}
	return output
}
func truncate(s string, n int) string {
	if len(s) > n {
		s = s[:n]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
	}
	return strings.ToValidUTF8(s, "\uFFFD")
}

func (s *Store) UserStats(ctx context.Context, userID int64) (map[string]any, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var submissions, accepted, solved, easy, medium, hard int64
	if err = tx.QueryRowContext(ctx, "SELECT count(*),COALESCE(sum(status='accepted'),0) FROM submissions WHERE user_id=?", userID).Scan(&submissions, &accepted); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(p.difficulty='easy'),0),COALESCE(sum(p.difficulty='medium'),0),COALESCE(sum(p.difficulty='hard'),0) FROM user_problem_status u JOIN problems p ON p.id=u.problem_id WHERE u.user_id=? AND u.status='solved'`, userID).Scan(&solved, &easy, &medium, &hard); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	rate := float64(0)
	if submissions > 0 {
		rate = 100 * float64(accepted) / float64(submissions)
	}
	return map[string]any{"solved": solved, "easy": easy, "medium": medium, "hard": hard, "submissions": submissions, "accepted": accepted, "acceptance_rate": rate}, nil
}
