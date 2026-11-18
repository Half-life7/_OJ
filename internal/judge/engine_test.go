package judge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"oj/internal/config"
	"oj/internal/judge/sandbox"
	"oj/internal/model"
)

type blockingSandbox struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
}

func (*blockingSandbox) Probe(context.Context, sandbox.Language) error { return nil }
func (*blockingSandbox) Compile(context.Context, sandbox.CompileRequest) (sandbox.CompileResult, error) {
	return sandbox.CompileResult{Status: "accepted", ArtifactID: "artifact"}, nil
}
func (s *blockingSandbox) Run(ctx context.Context, _ sandbox.RunRequest) (sandbox.RunResult, error) {
	s.startedOnce.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return sandbox.RunResult{}, ctx.Err()
	case <-s.release:
		return sandbox.RunResult{Status: "accepted", Stdout: "ok\n", TimeUs: 1234, MemoryKB: 256}, nil
	}
}
func (*blockingSandbox) Release(context.Context, string) error { return nil }

type instantSandbox struct{}

func (instantSandbox) Probe(context.Context, sandbox.Language) error { return nil }
func (instantSandbox) Compile(context.Context, sandbox.CompileRequest) (sandbox.CompileResult, error) {
	return sandbox.CompileResult{Status: "accepted", ArtifactID: "instant"}, nil
}
func (instantSandbox) Run(context.Context, sandbox.RunRequest) (sandbox.RunResult, error) {
	return sandbox.RunResult{Status: "accepted", Stdout: "ok\n", TimeUs: 900, MemoryKB: 512}, nil
}
func (instantSandbox) Release(context.Context, string) error { return nil }

func TestCloseDrainsActiveEvaluation(t *testing.T) {
	fake := &blockingSandbox{started: make(chan struct{}), release: make(chan struct{})}
	language := Language{
		ID: "test", Display: "Test", Source: "main.txt", Artifact: "main.txt",
		Compile: nil, Run: []string{"runner"}, Probe: []string{"probe"}, Template: "x",
		TimeFactor: 1, MemoryFactor: 1, ProcLimit: 1,
	}
	engine := New(nil, fake, map[string]Language{"test": language}, config.Config{Workers: 1, MaxCodeBytes: 65536})
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	problem := &model.Problem{
		ID: 1, Title: "close", TimeLimitMS: 1000, MemoryLimitMB: 128, Checker: "trim",
		Testcases: []model.Testcase{{Index: 1, Input: "", Expected: "ok\n", IsSample: true}},
	}
	resultCh := make(chan error, 1)
	go func() {
		result, err := engine.Evaluate(context.Background(), problem, "test", "source")
		if err == nil && result.Status != "accepted" {
			err = fmt.Errorf("unexpected status %s", result.Status)
		}
		resultCh <- err
	}()
	select {
	case <-fake.started:
	case <-time.After(2 * time.Second):
		t.Fatal("evaluation did not start")
	}
	closeResult := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { closeResult <- engine.Close(ctx) }()
	select {
	case err := <-closeResult:
		t.Fatalf("Close returned before active evaluation completed: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	close(fake.release)
	if err := <-resultCh; err != nil {
		t.Fatal(err)
	}
	if err := <-closeResult; err != nil {
		t.Fatal(err)
	}
	if engine.Healthy() {
		t.Fatal("closed engine still reports healthy")
	}
}

func TestSanitizeDiagnosticsRemovesSandboxPaths(t *testing.T) {
	input := "/box/private/main.cpp:4: error: bad\n/usr/include/c++/12/vector:10 failed\nsee https://example.com/a/b"
	got := sanitizeDiagnostics(input, 4096)
	for _, leaked := range []string{"/box/private", "/usr/include/c++"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("diagnostic leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "error: bad") || !strings.Contains(got, "https://example.com/a/b") {
		t.Fatalf("useful diagnostic content was lost: %s", got)
	}
}

func TestVerifyRunsEveryReferenceSolution(t *testing.T) {
	language := Language{
		ID: "test", Display: "Test", Source: "main.txt", Artifact: "main.txt",
		Run: []string{"runner"}, Probe: []string{"probe"}, Template: "x",
		TimeFactor: 1, MemoryFactor: 1, ProcLimit: 1,
	}
	engine := New(nil, instantSandbox{}, map[string]Language{"test": language}, config.Config{Workers: 1, MaxCodeBytes: 65536})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	problem := &model.Problem{
		ID: 1, Title: "verify", TimeLimitMS: 1000, MemoryLimitMB: 128, Checker: "trim",
		Testcases: []model.Testcase{
			{Index: 1, Input: "", Expected: "ok\n", IsSample: true},
			{Index: 2, Input: "", Expected: "ok\n"},
			{Index: 3, Input: "", Expected: "ok\n"},
		},
		Solutions: []model.Solution{
			{Language: "test", Code: "first"},
			{Language: "test", Code: "second"},
			{Language: "test", Code: "third"},
		},
	}
	result, err := engine.Verify(ctx, problem)
	if err != nil {
		t.Fatal(err)
	}
	verified, ok := result.(struct {
		Results []*model.Submission `json:"results"`
	})
	if !ok || len(verified.Results) != 3 {
		t.Fatalf("unexpected verify result %#v", result)
	}
	for _, submission := range verified.Results {
		if submission.Status != "accepted" || submission.Passed != 3 || submission.Total != 3 {
			t.Fatalf("reference verification failed: %+v", submission)
		}
	}
}
