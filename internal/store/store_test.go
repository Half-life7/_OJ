package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"oj/internal/model"
)

var testContext = context.Background()

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func addUser(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	r, err := s.DB.Exec("INSERT INTO users(username,password_hash,created_at) VALUES(?,'test-hash',?)", name, now())
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func fixtureProblem() model.Problem {
	return model.Problem{Title: "A+B", Difficulty: "easy", Description: "sum", InputFormat: "a b", OutputFormat: "sum", Hint: "hint", TimeLimitMS: 1000, MemoryLimitMB: 128, IsPublic: true, Tags: []string{"math", "basics"}, Testcases: []model.Testcase{{Input: "1 2", Expected: "3", IsSample: true, Explanation: "sum"}, {Input: "100 20", Expected: "120"}}, Templates: map[string]string{"cpp17": "template"}, LangLimits: map[string]model.Limits{"java": {TimeLimitMS: 2000}, "python3": {MemoryLimitMB: 256}}, Solutions: []model.Solution{{Language: "cpp17", Code: "answer"}}}
}
func addProblem(t *testing.T, s *Store) model.Problem {
	t.Helper()
	p := fixtureProblem()
	if err := s.SaveProblem(testContext, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func addSubmission(t *testing.T, s *Store, pid, uid int64) *model.Submission {
	t.Helper()
	sub := &model.Submission{ProblemID: pid, UserID: uid, Language: "cpp17", Code: "code"}
	if err := s.CreateSubmission(testContext, sub); err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestMigrationsReopenAndForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "数据库 #.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := addProblem(t, s)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.GetProblem(testContext, p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != 1 {
		t.Fatalf("migration count %d: %v", n, err)
	}
	if err = s.DB.QueryRow("PRAGMA foreign_keys").Scan(&n); err != nil || n != 1 {
		t.Fatalf("foreign keys %d: %v", n, err)
	}
	var mode string
	if err = s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal %s: %v", mode, err)
	}
	if _, err = s.DB.Exec("INSERT INTO testcases(problem_id,idx,input,expected,is_sample) VALUES(999,1,'','',0)"); err == nil {
		t.Fatal("foreign key not enforced")
	}
}

func TestProblemAssociationsAndRollback(t *testing.T) {
	s := testStore(t)
	p := addProblem(t, s)
	got, err := s.GetProblem(testContext, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 || len(got.Testcases) != 2 || got.TestcaseCount != 2 || got.Testcases[1].Index != 2 || got.Templates["cpp17"] != "template" || got.LangLimits["java"].MemoryLimitMB != 0 || len(got.Solutions) != 1 {
		t.Fatalf("roundtrip: %+v", got)
	}
	p.Title = "edited"
	p.Tags = []string{"only"}
	p.Testcases = p.Testcases[:1]
	p.Templates = nil
	p.Solutions = nil
	p.LangLimits = map[string]model.Limits{"java": {TimeLimitMS: -1}}
	if err = s.SaveProblem(testContext, &p); err == nil {
		t.Fatal("negative limit accepted")
	}
	got, _ = s.GetProblem(testContext, p.ID)
	if got.Title != "A+B" || len(got.Tags) != 2 || len(got.Testcases) != 2 || len(got.Solutions) != 1 {
		t.Fatal("partial edit persisted")
	}
	p.LangLimits = nil
	if err = s.SaveProblem(testContext, &p); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProblem(testContext, p.ID)
	if got.Title != "edited" || len(got.Tags) != 1 || len(got.Testcases) != 1 || len(got.Templates) != 0 || len(got.LangLimits) != 0 || len(got.Solutions) != 0 {
		t.Fatalf("old associations remain: %+v", got)
	}
	p.ID = 999
	if err = s.SaveProblem(testContext, &p); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing update: %v", err)
	}
}

func TestProblemFilteringAndPublicTags(t *testing.T) {
	s := testStore(t)
	p := addProblem(t, s)
	p.ID = 0
	p.IsPublic = false
	p.Title = "Private"
	p.Tags = []string{"secret"}
	if err := s.SaveProblem(testContext, &p); err != nil {
		t.Fatal(err)
	}
	items, n, err := s.ListProblems(testContext, ProblemFilter{})
	if err != nil || n != 1 || len(items) != 1 {
		t.Fatalf("public: %d %v", n, err)
	}
	if items[0].TestcaseCount != 2 || len(items[0].Testcases) != 0 || len(items[0].Solutions) != 0 {
		t.Fatal("list leaks test data or lost testcase count")
	}
	for _, f := range []ProblemFilter{{Query: "A+", Tag: "math", Difficulty: "easy"}, {Query: "1"}, {Admin: true, Query: "Private"}} {
		_, n, err = s.ListProblems(testContext, f)
		if err != nil || n != 1 {
			t.Fatalf("filter %+v: %d %v", f, n, err)
		}
	}
	for _, q := range []string{"%", "_", "' OR 1=1 --"} {
		_, n, err = s.ListProblems(testContext, ProblemFilter{Query: q})
		if err != nil || n != 0 {
			t.Fatalf("query %q: %d %v", q, n, err)
		}
	}
	tags, err := s.Tags(testContext)
	if err != nil || strings.Contains(strings.Join(tags, ","), "secret") {
		t.Fatal("private tag leaked")
	}
	items, n, err = s.ListProblems(testContext, ProblemFilter{Admin: true, Page: 2, PageSize: 1})
	if err != nil || n != 2 || len(items) != 1 {
		t.Fatal("paging failed")
	}
}

func TestAtomicSeed(t *testing.T) {
	s := testStore(t)
	good := fixtureProblem()
	bad := fixtureProblem()
	bad.LangLimits = map[string]model.Limits{"bad": {TimeLimitMS: -1}}
	if err := s.ImportProblemsIfEmpty(testContext, []model.Problem{good, bad}); err == nil {
		t.Fatal("bad seed accepted")
	}
	_, n, _ := s.ListProblems(testContext, ProblemFilter{Admin: true})
	if n != 0 {
		t.Fatal("partial seed persisted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.ImportProblemsIfEmpty(testContext, []model.Problem{good, good}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	_, n, _ = s.ListProblems(testContext, ProblemFilter{Admin: true})
	if n != 2 {
		t.Fatalf("seed duplicated: %d", n)
	}
}

func TestConcurrentAdmissionAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admission.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	uid := addUser(t, s, "alice")
	p := addProblem(t, s)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			db := s
			if i%2 == 0 {
				db = other
			}
			sub := &model.Submission{ProblemID: p.ID, UserID: uid, Language: "cpp17", Code: "x"}
			err := db.CreateSubmission(testContext, sub)
			if err == nil {
				admitted.Add(1)
			} else if !errors.Is(err, ErrTooManyPending) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if admitted.Load() != 3 {
		t.Fatalf("admitted %d", admitted.Load())
	}
	count, err := s.PendingCount(testContext, uid)
	if err != nil || count != 3 {
		t.Fatal(count, err)
	}
	got, _ := s.GetProblem(testContext, p.ID)
	if got.SubmitCount != 3 {
		t.Fatalf("counter %d", got.SubmitCount)
	}
	uid2 := addUser(t, s, "bob")
	addSubmission(t, s, p.ID, uid2)
	items, err := s.PendingSubmissions(testContext)
	if err != nil || len(items) != 4 {
		t.Fatal(items, err)
	}
	if err = s.MarkJudging(testContext, items[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.PendingCount(testContext, uid); n != 3 {
		t.Fatal("judging not counted")
	}
}

func TestFinishAtomicStatsIdempotenceAndRetention(t *testing.T) {
	s := testStore(t)
	uid := addUser(t, s, "alice")
	p := addProblem(t, s)
	sub := addSubmission(t, s, p.ID, uid)
	sub.Status = "accepted"
	sub.TimeUs = 1001
	sub.MemoryKB = 10
	sub.Passed = 2
	sub.Total = 2
	sub.CaseResults = []model.CaseResult{{Index: 1, Status: "accepted", TimeUs: 1, IsSample: true, Input: strings.Repeat("你", 1000)}, {Index: 2, Status: "accepted", Input: "hidden", Expected: "hidden", Actual: "hidden"}}
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_status BEFORE INSERT ON user_problem_status BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishSubmission(testContext, sub); err == nil {
		t.Fatal("expected failure")
	}
	got, _ := s.GetSubmission(testContext, sub.ID)
	problem, _ := s.GetProblem(testContext, p.ID)
	if got.Status != "pending" || problem.AcceptedCount != 0 {
		t.Fatal("finish partially committed")
	}
	s.DB.Exec("DROP TRIGGER fail_status")
	// Immutable identity comes from storage, never the supplied result object.
	sub.UserID = 999
	sub.ProblemID = 999
	if err := s.FinishSubmission(testContext, sub); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishSubmission(testContext, sub); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSubmission(testContext, sub.ID)
	if got.TimeMS != 2 || got.CaseResults[0].TimeMS != 1 || len(got.CaseResults[0].Input) > 2048 || got.CaseResults[1].Input != "" || got.CaseResults[1].Expected != "" || got.CaseResults[1].Actual != "" {
		t.Fatalf("result sanitization: %+v", got)
	}
	stats, err := s.UserStats(testContext, uid)
	if err != nil || stats["solved"] != int64(1) || stats["easy"] != int64(1) || stats["accepted"] != int64(1) || stats["acceptance_rate"] != float64(100) {
		t.Fatal(stats, err)
	}
	failed := addSubmission(t, s, p.ID, uid)
	failed.Status = "wrong_answer"
	failed.Total = 2
	if err = s.FinishSubmission(testContext, failed); err != nil {
		t.Fatal(err)
	}
	items, _, _ := s.ListProblems(testContext, ProblemFilter{UserID: uid})
	if items[0].Status != "solved" || items[0].AcceptedCount != 1 || items[0].SubmitCount != 2 {
		t.Fatal("solved status downgraded or counts wrong")
	}
	if err = s.MarkJudging(testContext, sub.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("terminal result requeued")
	}
	inflight := addSubmission(t, s, p.ID, uid)
	if err = s.DeleteProblem(testContext, p.ID); err != nil {
		t.Fatal(err)
	}
	inflight.Status = "system_error"
	if err = s.FinishSubmission(testContext, inflight); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetSubmission(testContext, sub.ID)
	if err != nil || got.ProblemID != p.ID || !strings.Contains(got.ProblemTitle, "已删除") {
		t.Fatalf("lost history: %+v %v", got, err)
	}
	for _, table := range []string{"problem_tags", "testcases", "problem_templates", "problem_lang_limits", "problem_solutions", "user_problem_status"} {
		var n int
		if err = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	history, n, err := s.ListSubmissions(testContext, SubmissionFilter{UserID: uid, Status: "accepted"})
	if err != nil || n != 1 || len(history) != 1 {
		t.Fatal(n, err)
	}
}

func TestCancelledOperationsAndMissingRows(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithCancel(testContext)
	cancel()
	p := fixtureProblem()
	if err := s.SaveProblem(ctx, &p); err == nil {
		t.Fatal("cancelled operation succeeded")
	}
	if _, err := s.GetProblem(testContext, 123); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if _, err := s.GetSubmission(testContext, 123); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if err := s.DeleteProblem(testContext, 123); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if err := s.FinishSubmission(testContext, &model.Submission{ID: 123, Status: "pending"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal(err)
	}
}
