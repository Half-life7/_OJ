package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"oj/internal/model"
)

const problemColumns = `p.id,p.title,p.difficulty,p.description,p.input_format,p.output_format,p.hint,p.time_limit_ms,p.memory_limit_mb,p.checker,p.float_eps,p.is_public,p.created_by,p.submit_count,p.accepted_count,p.created_at,p.updated_at,(SELECT count(*) FROM testcases tc WHERE tc.problem_id=p.id)`

type scanner interface{ Scan(...any) error }

func scanProblem(row scanner, p *model.Problem, status bool) error {
	dest := []any{&p.ID, &p.Title, &p.Difficulty, &p.Description, &p.InputFormat, &p.OutputFormat, &p.Hint, &p.TimeLimitMS, &p.MemoryLimitMB, &p.Checker, &p.FloatEps, &p.IsPublic, &p.CreatedBy, &p.SubmitCount, &p.AcceptedCount, &p.CreatedAt, &p.UpdatedAt, &p.TestcaseCount}
	if status {
		dest = append(dest, &p.Status)
	}
	return row.Scan(dest...)
}

func (s *Store) GetProblem(ctx context.Context, id int64) (*model.Problem, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p := new(model.Problem)
	if err = scanProblem(tx.QueryRowContext(ctx, "SELECT "+problemColumns+" FROM problems p WHERE p.id=?", id), p, false); err != nil {
		return nil, err
	}
	if err = loadAssociations(ctx, tx, p, true); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListProblems(ctx context.Context, f ProblemFilter) ([]model.Problem, int, error) {
	where, args := " WHERE 1=1", []any{}
	if !f.Admin {
		where += " AND p.is_public=1"
	}
	if f.Query != "" {
		where += ` AND (p.title LIKE ? ESCAPE '\' OR CAST(p.id AS TEXT)=?)`
		args = append(args, "%"+escapeLike(f.Query)+"%", f.Query)
	}
	if f.Difficulty != "" {
		where += " AND p.difficulty=?"
		args = append(args, f.Difficulty)
	}
	if f.Tag != "" {
		where += " AND EXISTS(SELECT 1 FROM problem_tags t WHERE t.problem_id=p.id AND t.tag=?)"
		args = append(args, f.Tag)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM problems p"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := paging(f.Page, f.PageSize)
	queryArgs := append([]any{f.UserID}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := tx.QueryContext(ctx, "SELECT "+problemColumns+",COALESCE((SELECT status FROM user_problem_status u WHERE u.problem_id=p.id AND u.user_id=?),'') FROM problems p"+where+" ORDER BY p.id LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	items := []model.Problem{}
	for rows.Next() {
		var p model.Problem
		if err = scanProblem(rows, &p, true); err != nil {
			rows.Close()
			return nil, 0, err
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		if err = loadAssociations(ctx, tx, &items[i], false); err != nil {
			return nil, 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func loadAssociations(ctx context.Context, q querier, p *model.Problem, full bool) error {
	p.Tags = []string{}
	p.Testcases = []model.Testcase{}
	p.Templates = map[string]string{}
	p.LangLimits = map[string]model.Limits{}
	p.Solutions = []model.Solution{}
	rows, err := q.QueryContext(ctx, "SELECT tag FROM problem_tags WHERE problem_id=? ORDER BY tag", p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tag string
		if err = rows.Scan(&tag); err != nil {
			rows.Close()
			return err
		}
		p.Tags = append(p.Tags, tag)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !full {
		return err
	}
	rows, err = q.QueryContext(ctx, "SELECT idx,input,expected,is_sample,explanation FROM testcases WHERE problem_id=? ORDER BY idx", p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tc model.Testcase
		if err = rows.Scan(&tc.Index, &tc.Input, &tc.Expected, &tc.IsSample, &tc.Explanation); err != nil {
			rows.Close()
			return err
		}
		p.Testcases = append(p.Testcases, tc)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, "SELECT language,code FROM problem_templates WHERE problem_id=?", p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var lang, code string
		if err = rows.Scan(&lang, &code); err != nil {
			rows.Close()
			return err
		}
		p.Templates[lang] = code
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, "SELECT language,COALESCE(time_limit_ms,0),COALESCE(memory_limit_mb,0) FROM problem_lang_limits WHERE problem_id=?", p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var lang string
		var l model.Limits
		if err = rows.Scan(&lang, &l.TimeLimitMS, &l.MemoryLimitMB); err != nil {
			rows.Close()
			return err
		}
		p.LangLimits[lang] = l
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, "SELECT language,code FROM problem_solutions WHERE problem_id=? ORDER BY language", p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sol model.Solution
		if err = rows.Scan(&sol.Language, &sol.Code); err != nil {
			rows.Close()
			return err
		}
		p.Solutions = append(p.Solutions, sol)
	}
	err = rows.Err()
	rows.Close()
	return err
}

func (s *Store) SaveProblem(ctx context.Context, p *model.Problem) error {
	if p == nil {
		return errors.New("nil problem")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	copy := *p
	if err = saveProblem(ctx, tx, &copy); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*p = copy
	return nil
}

// ImportProblemsIfEmpty checks emptiness and imports the complete batch in one
// write transaction, including every association. Partial seeds never commit.
func (s *Store) ImportProblemsIfEmpty(ctx context.Context, problems []model.Problem) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM problems").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return tx.Commit()
	}
	for _, p := range problems {
		p.ID = 0
		if err = saveProblem(ctx, tx, &p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func saveProblem(ctx context.Context, tx *sql.Tx, p *model.Problem) error {
	if p.Checker == "" {
		p.Checker = "trim"
	}
	if p.FloatEps == 0 {
		p.FloatEps = 1e-6
	}
	p.UpdatedAt = now()
	args := []any{p.Title, p.Difficulty, p.Description, p.InputFormat, p.OutputFormat, p.Hint, p.TimeLimitMS, p.MemoryLimitMB, p.Checker, p.FloatEps, p.IsPublic, p.UpdatedAt}
	if p.ID == 0 {
		p.CreatedAt = p.UpdatedAt
		p.SubmitCount = 0
		p.AcceptedCount = 0
		args = append(args, p.CreatedBy, p.CreatedAt)
		res, err := tx.ExecContext(ctx, `INSERT INTO problems(title,difficulty,description,input_format,output_format,hint,time_limit_ms,memory_limit_mb,checker,float_eps,is_public,updated_at,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
		if err != nil {
			return err
		}
		p.ID, err = res.LastInsertId()
		if err != nil {
			return err
		}
	} else {
		if err := tx.QueryRowContext(ctx, "SELECT created_by,created_at,submit_count,accepted_count FROM problems WHERE id=?", p.ID).Scan(&p.CreatedBy, &p.CreatedAt, &p.SubmitCount, &p.AcceptedCount); err != nil {
			return err
		}
		args = append(args, p.ID)
		if err := changed(tx.ExecContext(ctx, `UPDATE problems SET title=?,difficulty=?,description=?,input_format=?,output_format=?,hint=?,time_limit_ms=?,memory_limit_mb=?,checker=?,float_eps=?,is_public=?,updated_at=? WHERE id=?`, args...)); err != nil {
			return err
		}
		for _, table := range []string{"problem_tags", "testcases", "problem_templates", "problem_lang_limits", "problem_solutions"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE problem_id=?", p.ID); err != nil {
				return err
			}
		}
	}
	for _, tag := range p.Tags {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO problem_tags(problem_id,tag) VALUES(?,?)", p.ID, tag); err != nil {
			return err
		}
	}
	for i, tc := range p.Testcases {
		if _, err := tx.ExecContext(ctx, "INSERT INTO testcases(problem_id,idx,input,expected,is_sample,explanation) VALUES(?,?,?,?,?,?)", p.ID, i+1, tc.Input, tc.Expected, tc.IsSample, tc.Explanation); err != nil {
			return err
		}
	}
	p.TestcaseCount = len(p.Testcases)
	for lang, code := range p.Templates {
		if _, err := tx.ExecContext(ctx, "INSERT INTO problem_templates(problem_id,language,code) VALUES(?,?,?)", p.ID, lang, code); err != nil {
			return err
		}
	}
	for lang, l := range p.LangLimits {
		if l.TimeLimitMS < 0 || l.MemoryLimitMB < 0 {
			return fmt.Errorf("language limits cannot be negative")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO problem_lang_limits(problem_id,language,time_limit_ms,memory_limit_mb) VALUES(?,?,NULLIF(?,0),NULLIF(?,0))", p.ID, lang, l.TimeLimitMS, l.MemoryLimitMB); err != nil {
			return err
		}
	}
	for _, sol := range p.Solutions {
		if _, err := tx.ExecContext(ctx, "INSERT INTO problem_solutions(problem_id,language,code) VALUES(?,?,?)", p.ID, sol.Language, sol.Code); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteProblem(ctx context.Context, id int64) error {
	return changed(s.DB.ExecContext(ctx, "DELETE FROM problems WHERE id=?", id))
}
func (s *Store) Tags(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT t.tag FROM problem_tags t JOIN problems p ON p.id=t.problem_id WHERE p.is_public=1 ORDER BY t.tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var tag string
		if err = rows.Scan(&tag); err != nil {
			return nil, err
		}
		items = append(items, tag)
	}
	return items, rows.Err()
}
