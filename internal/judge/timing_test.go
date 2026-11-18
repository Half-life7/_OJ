package judge

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"oj/internal/config"
	"oj/internal/judge/sandbox"
	"oj/internal/model"
)

func TestCPUTimingStabilityOnLinux(t *testing.T) {
	if os.Getenv("OJ_TIMING_TEST") != "1" {
		t.Skip("set OJ_TIMING_TEST=1 on an exclusive Linux judge host")
	}
	if runtime.GOOS != "linux" {
		t.Skip("timing verification only runs on Linux")
	}
	languageFile, err := filepath.Abs(filepath.Join("..", "..", "languages.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	languages, err := LoadLanguages(languageFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LanguagesFile = languageFile
	if cfg.Workers > 1 {
		cfg.Workers = 1
	}
	engine := New(nil, sandbox.NewGoJudge(cfg.SandboxURL, cfg.SandboxToken), languages, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer closeCancel()
		_ = engine.Close(closeCtx)
	}()
	if !engine.Healthy() {
		t.Skip("go-judge unavailable")
	}
	program := `#include <bits/stdc++.h>
using namespace std; volatile unsigned long long sink; int main(){unsigned long long x=1;for(unsigned long long i=0;i<150000000ULL;i++)x=x*1099511628211ULL+i;sink=x;cout<<"ok\n";}`
	problem := &model.Problem{ID: 1, TimeLimitMS: 30000, MemoryLimitMB: 256, Checker: "trim", FloatEps: 1e-6,
		Testcases: []model.Testcase{{Index: 1, Expected: "ok\n", IsSample: true}}}
	measurements := make([]float64, 0, 7)
	for i := 0; i < 7; i++ {
		result, evaluateErr := engine.Evaluate(ctx, problem, "cpp17", program)
		if evaluateErr != nil || result.Status != "accepted" {
			t.Fatalf("run %d: status=%v err=%v", i+1, result, evaluateErr)
		}
		measurements = append(measurements, float64(result.TimeUs)/1000)
	}
	mean := 0.0
	for _, value := range measurements {
		mean += value
	}
	mean /= float64(len(measurements))
	variance := 0.0
	for _, value := range measurements {
		variance += (value - mean) * (value - mean)
	}
	cv := math.Sqrt(variance/float64(len(measurements))) / mean * 100
	threshold := 5.0
	if raw := os.Getenv("OJ_TIMING_CV_PERCENT"); raw != "" {
		threshold, err = strconv.ParseFloat(raw, 64)
		if err != nil || threshold <= 0 {
			t.Fatalf("invalid OJ_TIMING_CV_PERCENT")
		}
	}
	if cv > threshold {
		t.Fatalf("CPU timing CV %.2f%% exceeds %.2f%%; check cpuset, governor, SMT, Turbo, virtualization and background load", cv, threshold)
	}
}
