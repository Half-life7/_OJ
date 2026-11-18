package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"oj/internal/config"
	"oj/internal/judge"
	"oj/internal/judge/sandbox"
)

// This test is intentionally opt-in because it starts real untrusted execution
// through go-judge and requires all three toolchains in the sandbox.
func TestAllReferenceSolutionsAcceptedByRealSandbox(t *testing.T) {
	if os.Getenv("OJ_SANDBOX_INTEGRATION") != "1" {
		t.Skip("set OJ_SANDBOX_INTEGRATION=1 with a real go-judge service")
	}
	languageFile, err := filepath.Abs(filepath.Join("..", "languages.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	languages, err := judge.LoadLanguages(languageFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SandboxURL:   envOr("SANDBOX_URL", "http://127.0.0.1:5050"),
		SandboxToken: os.Getenv("SANDBOX_TOKEN"),
		Workers:      1, MaxCodeBytes: 65536,
	}
	engine := judge.New(nil, sandbox.NewGoJudge(cfg.SandboxURL, cfg.SandboxToken), languages, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer closeCancel()
		_ = engine.Close(closeCtx)
	}()
	if !engine.Healthy() {
		t.Skip("go-judge is unreachable or no language toolchain is available")
	}
	problems, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	available := map[string]bool{}
	for _, language := range engine.Available() {
		available[language.ID] = true
	}
	for _, problem := range problems {
		problem := problem
		for _, solution := range problem.Solutions {
			solution := solution
			t.Run(problem.Title+"/"+solution.Language, func(t *testing.T) {
				if !available[solution.Language] {
					t.Skipf("%s toolchain is unavailable in the sandbox", solution.Language)
				}
				result, evaluateErr := engine.Evaluate(ctx, &problem, solution.Language, solution.Code)
				if evaluateErr != nil {
					t.Fatal(evaluateErr)
				}
				if result.Status != "accepted" || result.Passed != result.Total {
					t.Fatalf("status=%s passed=%d/%d message=%s", result.Status, result.Passed, result.Total, result.Message)
				}
			})
		}
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
