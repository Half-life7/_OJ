// Command bench measures CPU-time stability through the same judge engine and
// go-judge adapter used by submissions. It never starts user code locally.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"oj/internal/config"
	"oj/internal/judge"
	"oj/internal/judge/sandbox"
	"oj/internal/model"
)

type sample struct {
	CPUMS  float64
	WallMS float64
}

type report struct {
	Count  int     `json:"count"`
	MinMS  float64 `json:"min_ms"`
	MeanMS float64 `json:"mean_ms"`
	MaxMS  float64 `json:"max_ms"`
	StdMS  float64 `json:"stddev_ms"`
	CV     float64 `json:"cv"`
}

var programs = map[string]string{
	"cpp17": `#include <bits/stdc++.h>
using namespace std;
volatile unsigned long long sink;
int main() {
    unsigned long long x = 0x9e3779b97f4a7c15ULL;
    for (unsigned long long i = 0; i < 240000000ULL; ++i) {
        x ^= i + 0x9e3779b97f4a7c15ULL + (x << 6) + (x >> 2);
        x *= 1099511628211ULL;
    }
    sink = x;
    cout << "ok\n";
    return 0;
}
`,
	"python3": `import time
x = 0x9e3779b97f4a7c15
mask = (1 << 64) - 1
for i in range(9000000):
    x ^= (i + 0x9e3779b97f4a7c15 + ((x << 6) & mask) + (x >> 2)) & mask
    x = (x * 1099511628211) & mask
print("ok")
`,
	"java": `public class Main {
    static volatile long sink;
    public static void main(String[] args) {
        long x = 0x9e3779b97f4a7c15L;
        for (long i = 0; i < 240000000L; ++i) {
            x ^= i + 0x9e3779b97f4a7c15L + (x << 6) + (x >> 2);
            x *= 1099511628211L;
        }
        sink = x;
        System.out.println("ok");
    }
}
`,
}

func main() {
	languageID := flag.String("lang", "cpp17", "language ID from languages.yaml (cpp17, python3, java)")
	count := flag.Int("n", 30, "number of measured runs for each serial and concurrent group")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall benchmark timeout")
	flag.Parse()
	if *count < 2 || *count > 1000 {
		fatalf("-n must be between 2 and 1000")
	}
	program, ok := programs[*languageID]
	if !ok {
		fatalf("unsupported benchmark language %q", *languageID)
	}
	cfg, err := config.Load()
	if err != nil {
		fatalf("configuration: %v", err)
	}
	if cfg.Sandbox != "gojudge" {
		fatalf("bench requires SANDBOX=gojudge; isolate is a fail-closed placeholder")
	}
	languages, err := judge.LoadLanguagesOrDefault(cfg.LanguagesFile)
	if err != nil {
		fatalf("languages: %v", err)
	}
	language, ok := languages[*languageID]
	if !ok {
		fatalf("language %q is not configured", *languageID)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, *timeout)
	defer deadline()
	engine := judge.New(nil, sandbox.NewGoJudge(cfg.SandboxURL, cfg.SandboxToken), languages, cfg)
	if err = engine.Start(ctx); err != nil {
		fatalf("start judge engine: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer closeCancel()
		_ = engine.Close(closeCtx)
	}()
	waitCtx, waitCancel := context.WithTimeout(ctx, 20*time.Second)
	defer waitCancel()
	for !engine.Healthy() {
		select {
		case <-waitCtx.Done():
			fatalf("sandbox or language toolchain is unavailable at %s; start go-judge and verify %s", cfg.SandboxURL, language.Probe)
		case <-time.After(100 * time.Millisecond):
		}
	}
	available := false
	for _, candidate := range engine.Available() {
		available = available || candidate.ID == *languageID
	}
	if !available {
		fatalf("sandbox probe did not make %s available", *languageID)
	}
	printWarnings(cfg)
	problem := &model.Problem{
		ID: 1, Title: "CPU stability benchmark", Difficulty: "easy",
		TimeLimitMS: 30000, MemoryLimitMB: 512, Checker: "trim", FloatEps: 1e-6,
		Testcases: []model.Testcase{{Index: 1, Input: "", Expected: "ok\n", IsSample: true}},
	}
	fmt.Printf("benchmark: lang=%s workers=%d cpuset=%q n=%d\n", *languageID, cfg.Workers, strings.Join(cfg.CPUSet, ","), *count)
	fmt.Println("warming up (not included in statistics)…")
	if _, err = runOne(ctx, engine, problem, *languageID, program); err != nil {
		fatalf("warm-up failed: %v", err)
	}
	fmt.Println("serial runs…")
	serial := make([]sample, 0, *count)
	for i := 0; i < *count; i++ {
		item, runErr := runOne(ctx, engine, problem, *languageID, program)
		if runErr != nil {
			fatalf("serial run %d/%d: %v", i+1, *count, runErr)
		}
		serial = append(serial, item)
		fmt.Printf("\r  %d/%d", i+1, *count)
	}
	fmt.Println()
	fmt.Printf("concurrent runs (%d workers)…\n", cfg.Workers)
	concurrent, err := runConcurrent(ctx, engine, problem, *languageID, program, *count, cfg.Workers)
	if err != nil {
		fatalf("concurrent runs: %v", err)
	}
	serialReport := summarize(serial)
	concurrentReport := summarize(concurrent)
	fmt.Println()
	fmt.Printf("serial:     min=%.3f ms mean=%.3f ms max=%.3f ms stddev=%.3f ms CV=%.2f%%\n",
		serialReport.MinMS, serialReport.MeanMS, serialReport.MaxMS, serialReport.StdMS, serialReport.CV)
	fmt.Printf("concurrent: min=%.3f ms mean=%.3f ms max=%.3f ms stddev=%.3f ms CV=%.2f%%\n",
		concurrentReport.MinMS, concurrentReport.MeanMS, concurrentReport.MaxMS, concurrentReport.StdMS, concurrentReport.CV)
	delta := (concurrentReport.MeanMS - serialReport.MeanMS) / serialReport.MeanMS * 100
	fmt.Printf("concurrent mean delta: %+.2f%%\n", delta)
	fmt.Println()
	printDiagnosis(cfg, serialReport, concurrentReport, delta)
}

func runOne(ctx context.Context, engine *judge.Engine, problem *model.Problem, language, code string) (sample, error) {
	started := time.Now()
	result, err := engine.Evaluate(ctx, problem, language, code)
	wall := float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		return sample{}, err
	}
	if result.Status != "accepted" {
		return sample{}, fmt.Errorf("judge returned %s: %s", result.Status, result.Message)
	}
	if result.TimeUs <= 0 {
		return sample{}, errors.New("sandbox returned a non-positive CPU time")
	}
	return sample{CPUMS: float64(result.TimeUs) / 1000, WallMS: wall}, nil
}

func runConcurrent(ctx context.Context, engine *judge.Engine, problem *model.Problem, language, code string, count, workers int) ([]sample, error) {
	if workers < 1 {
		workers = 1
	}
	results := make([]sample, count)
	errs := make([]error, count)
	next := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range next {
				results[index], errs[index] = runOne(ctx, engine, problem, language, code)
			}
		}()
	}
	for index := 0; index < count; index++ {
		select {
		case <-ctx.Done():
			close(next)
			wg.Wait()
			return nil, ctx.Err()
		case next <- index:
		}
		fmt.Printf("\r  %d/%d", index+1, count)
	}
	close(next)
	wg.Wait()
	fmt.Println()
	for index, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("run %d: %w", index+1, err)
		}
	}
	return results, nil
}

func summarize(values []sample) report {
	result := report{Count: len(values)}
	if len(values) == 0 {
		return result
	}
	times := make([]float64, len(values))
	for i, item := range values {
		times[i] = item.CPUMS
		result.MeanMS += item.CPUMS
	}
	sort.Float64s(times)
	result.MinMS, result.MaxMS = times[0], times[len(times)-1]
	result.MeanMS /= float64(len(times))
	var variance float64
	for _, value := range times {
		delta := value - result.MeanMS
		variance += delta * delta
	}
	result.StdMS = math.Sqrt(variance / float64(len(times)))
	if result.MeanMS > 0 {
		result.CV = result.StdMS / result.MeanMS * 100
	}
	return result
}

func printWarnings(cfg config.Config) {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "警告：当前为 Windows 环境；go-judge Windows 兼容层仅适合冒烟，计时与隔离结论无效。")
	}
	if isWSL() {
		fmt.Fprintln(os.Stderr, "警告：检测到 WSL2；虚拟化环境不能作为 Linux 实机计时基准。")
	}
	if len(cfg.CPUSet) == 0 {
		fmt.Fprintln(os.Stderr, "警告：JUDGE_CPUSET 为空，未绑核，计时可能不稳定。")
	}
	if runtime.GOOS == "linux" {
		for _, cpu := range cfg.CPUSet {
			governorPath := filepath.Join("/sys/devices/system/cpu", "cpu"+cpu, "cpufreq", "scaling_governor")
			if value, err := os.ReadFile(governorPath); err == nil && strings.TrimSpace(string(value)) != "performance" {
				fmt.Fprintf(os.Stderr, "警告：CPU %s 的 governor=%s；建议设置为 performance。\n", cpu, strings.TrimSpace(string(value)))
			}
		}
		if value, err := os.ReadFile(filepath.Join("/", "sys", "devices", "system", "cpu", "intel_pstate", "no_turbo")); err == nil && strings.TrimSpace(string(value)) == "0" {
			fmt.Fprintln(os.Stderr, "警告：Intel Turbo 可能开启；卡常题建议固定频率。")
		}
		if hasSMT(cfg.CPUSet) {
			fmt.Fprintln(os.Stderr, "警告：所选逻辑核中存在 SMT 兄弟线程；建议每个物理核只分配一个逻辑核。")
		}
		if isVirtualizedLinux() {
			fmt.Fprintln(os.Stderr, "警告：检测到虚拟化环境；计时噪声通常高于独占裸机。")
		}
	}
}

func isWSL() bool {
	data, err := os.ReadFile(filepath.Join("/", "proc", "version"))
	return err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft")
}

func isVirtualizedLinux() bool {
	if _, err := os.Stat(filepath.Join("/", ".dockerenv")); err == nil {
		return true
	}
	data, err := os.ReadFile(filepath.Join("/", "proc", "cpuinfo"))
	return err == nil && strings.Contains(strings.ToLower(string(data)), "hypervisor")
}

func hasSMT(cpus []string) bool {
	seen := map[string]string{}
	for _, cpu := range cpus {
		data, err := os.ReadFile(filepath.Join("/sys/devices/system/cpu", "cpu"+cpu, "topology", "thread_siblings_list"))
		if err != nil {
			continue
		}
		key := strings.TrimSpace(string(data))
		if previous, ok := seen[key]; ok && previous != cpu {
			return true
		}
		seen[key] = cpu
	}
	return false
}

func printDiagnosis(cfg config.Config, serial, concurrent report, delta float64) {
	var hints []string
	if serial.CV >= 3 {
		hints = append(hints, "单次 CPU 时间 CV 未达到 3%，检查 governor、Turbo、温度和后台负载。")
	}
	if math.Abs(delta) >= 5 {
		hints = append(hints, "并发与单次均值差异未达到 5%，检查是否未绑核、共享 L3/SMT 或 cpuset 配置错误。")
	}
	if len(cfg.CPUSet) == 0 {
		hints = append(hints, "优先配置 JUDGE_CPUSET，每个 worker 独占一个逻辑核。")
	}
	if isWSL() || runtime.GOOS == "windows" {
		hints = append(hints, "WSL2/Windows 结果只用于功能联调；最终结论必须在 Linux 独占核实机复测。")
	}
	if len(hints) == 0 {
		fmt.Println("结论：本次环境达到建议目标；生产环境仍应以真实判题机上的重复测试为准。")
		return
	}
	fmt.Println("可能原因与建议：")
	for _, hint := range hints {
		fmt.Println("- " + hint)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bench: "+format+"\n", args...)
	os.Exit(1)
}
