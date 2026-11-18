package judge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"oj/internal/config"
	"oj/internal/judge/sandbox"
	"oj/internal/model"
	"oj/internal/store"
)

var (
	ErrUnavailable       = errors.New("judge unavailable")
	ErrQueueFull         = errors.New("judge queue full")
	ErrTooMany           = errors.New("too many pending submissions")
	ErrLanguage          = errors.New("language unavailable")
	ErrInvalidSubmission = errors.New("invalid submission")
)

const (
	queueCapacity = 128
	probeSlotWait = 4 * time.Second
	// go-judge permits up to 11 seconds of wall time for a 5-second probe.
	probeRequestTimeout = 15 * time.Second
)

// Engine owns the only execution slot pool. A slot is held across compilation,
// every testcase and artifact cleanup, including self-tests, verification and bench.
type Engine struct {
	store           *store.Store
	sb              sandbox.Sandbox
	langs           map[string]Language
	config          config.Config
	slots           chan string
	queue           chan model.Submission
	reserved        chan struct{}
	admission       sync.Mutex
	known           map[int64]bool
	healthMu        sync.RWMutex
	available       map[string]bool
	lifeMu          sync.Mutex
	started, closed bool
	ctx             context.Context
	cancel          context.CancelFunc
	serviceCtx      context.Context
	serviceCancel   context.CancelFunc
	stopCh          chan struct{}
	closeOnce       sync.Once
	wg              sync.WaitGroup
	done            chan struct{}
}

func New(s *store.Store, sb sandbox.Sandbox, langs map[string]Language, c config.Config) *Engine {
	if c.Workers < 1 {
		c.Workers = 1
	}
	if c.MaxCodeBytes < 1 {
		c.MaxCodeBytes = 65536
	}
	copyLangs := make(map[string]Language, len(langs))
	for id, l := range langs {
		copyLangs[id] = l
	}
	return &Engine{store: s, sb: sb, langs: copyLangs, config: c,
		slots: make(chan string, c.Workers), queue: make(chan model.Submission, queueCapacity),
		reserved: make(chan struct{}, queueCapacity), known: make(map[int64]bool),
		available: make(map[string]bool), stopCh: make(chan struct{}), done: make(chan struct{})}
}

func (e *Engine) Start(ctx context.Context) error {
	e.lifeMu.Lock()
	if e.closed {
		e.lifeMu.Unlock()
		return ErrUnavailable
	}
	if e.started {
		e.lifeMu.Unlock()
		return nil
	}
	if e.sb == nil {
		e.lifeMu.Unlock()
		return ErrUnavailable
	}
	if len(e.config.CPUSet) > 0 {
		if len(e.config.CPUSet) < e.config.Workers {
			e.lifeMu.Unlock()
			return fmt.Errorf("workers exceed CPUSet size")
		}
		seen := map[int]bool{}
		for _, core := range e.config.CPUSet {
			n, err := strconv.Atoi(core)
			if err != nil || n < 0 || seen[n] {
				e.lifeMu.Unlock()
				return fmt.Errorf("CPUSet must contain distinct logical core numbers")
			}
			seen[n] = true
		}
	}
	e.ctx, e.cancel = context.WithCancel(ctx)
	e.serviceCtx, e.serviceCancel = context.WithCancel(e.ctx)
	for i := 0; i < e.config.Workers; i++ {
		core := ""
		if len(e.config.CPUSet) > 0 {
			core = e.config.CPUSet[i]
		}
		e.slots <- core
	}
	e.started = true
	// Count startup itself so Close cannot finish while probing.
	e.wg.Add(1)
	e.lifeMu.Unlock()
	defer e.wg.Done()
	if len(e.config.CPUSet) == 0 {
		slog.Warn("未配置判题 CPU 亲和性；计时可能不稳定")
	}
	if e.config.SandboxToken == "" {
		slog.Warn("未配置 SANDBOX_TOKEN；请确保 go-judge 只监听本机内部网络")
	}
	e.probe()
	e.lifeMu.Lock()
	if e.closed || e.ctx.Err() != nil {
		e.lifeMu.Unlock()
		return nil
	}
	for i := 0; i < e.config.Workers; i++ {
		e.wg.Add(1)
		go e.worker()
	}
	e.wg.Add(1)
	go e.maintain()
	e.lifeMu.Unlock()
	e.recoverPending()
	return nil // Sandbox availability never prevents the HTTP service starting.
}

func (e *Engine) Close(ctx context.Context) error {
	e.lifeMu.Lock()
	if !e.closed {
		e.closed = true
	}
	started := e.started
	if e.serviceCancel != nil {
		e.serviceCancel()
	}
	e.lifeMu.Unlock()
	if !started {
		return nil
	}
	e.closeOnce.Do(func() {
		// Stop admission first, then wake workers. Jobs already accepted remain
		// in the queue and are drained before the graceful context expires.
		e.admission.Lock()
		e.admission.Unlock()
		close(e.stopCh)
		go func() {
			e.wg.Wait()
			if e.cancel != nil {
				e.cancel()
			}
			close(e.done)
		}()
	})
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) begin(ctx context.Context) (context.Context, func(), error) {
	e.lifeMu.Lock()
	defer e.lifeMu.Unlock()
	if !e.started || e.closed || e.ctx.Err() != nil {
		return nil, nil, ErrUnavailable
	}
	e.wg.Add(1)
	op, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	return op, func() { stop(); cancel(); e.wg.Done() }, nil
}

func (e *Engine) Healthy() bool {
	e.lifeMu.Lock()
	running := e.started && !e.closed && e.ctx.Err() == nil
	e.lifeMu.Unlock()
	if !running {
		return false
	}
	e.healthMu.RLock()
	defer e.healthMu.RUnlock()
	return len(e.available) > 0
}

func (e *Engine) Available() []Language {
	e.healthMu.RLock()
	defer e.healthMu.RUnlock()
	out := make([]Language, 0, len(e.available))
	for id := range e.available {
		l := e.langs[id]
		l.Compile = nil
		l.Run = nil
		l.Probe = nil
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (e *Engine) language(id string) (Language, error) {
	l, ok := e.langs[id]
	if !ok {
		return Language{}, ErrLanguage
	}
	e.healthMu.RLock()
	ok = e.available[id]
	e.healthMu.RUnlock()
	if !ok {
		return Language{}, ErrUnavailable
	}
	return l, nil
}

func (e *Engine) acquire(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case core := <-e.slots:
		if ctx.Err() != nil {
			e.slots <- core
			return "", ctx.Err()
		}
		return core, nil
	}
}

func (e *Engine) probe() {
	ids := make([]string, 0, len(e.langs))
	for id := range e.langs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		slotCtx, cancelSlot := context.WithTimeout(e.serviceCtx, probeSlotWait)
		core, err := e.acquire(slotCtx)
		cancelSlot()
		if err != nil {
			// A busy judge has not reported a toolchain failure.
			continue
		}
		started := time.Now()
		probeCtx, cancelProbe := context.WithTimeout(e.serviceCtx, probeRequestTimeout)
		err = e.sb.Probe(probeCtx, e.langs[id])
		cancelProbe()
		e.healthMu.Lock()
		if err == nil {
			e.available[id] = true
		} else {
			delete(e.available, id)
		}
		e.healthMu.Unlock()
		e.slots <- core
		if err != nil && e.serviceCtx.Err() == nil {
			log := slog.With("language", id, "elapsed", time.Since(started), "error", err)
			var failure *sandbox.ProbeFailure
			if errors.As(err, &failure) && failure.Detail != "" {
				log = log.With("diagnostic", sanitizeDiagnostics(failure.Detail, 768))
			}
			log.Warn("sandbox language probe failed")
		}
	}
}

func (e *Engine) maintain() {
	defer e.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	probeTicks := 0
	for {
		select {
		case <-e.serviceCtx.Done():
			return
		case <-ticker.C:
			// Retry unavailable toolchains promptly; healthy services need fewer probes.
			e.healthMu.RLock()
			availableCount := len(e.available)
			missing := availableCount != len(e.langs)
			e.healthMu.RUnlock()
			probeTicks++
			switch {
			case availableCount == 0 && probeTicks < 5:
				// Avoid hammering an unreachable sandbox and flooding logs.
			case missing || probeTicks >= 15:
				e.probe()
				probeTicks = 0
			}
			e.recoverPending()
		}
	}
}

// Submit reserves bounded queue capacity BEFORE the transactional DB insert.
// The mutex also coordinates recovery so an ID can never be enqueued twice.
func (e *Engine) Submit(ctx context.Context, sub *model.Submission) error {
	op, done, err := e.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	if sub == nil || sub.ID != 0 || sub.UserID <= 0 || sub.ProblemID <= 0 || len(sub.Code) == 0 || int64(len(sub.Code)) > e.config.MaxCodeBytes {
		return ErrInvalidSubmission
	}
	if _, err = e.language(sub.Language); err != nil {
		return err
	}
	if e.store == nil {
		return ErrUnavailable
	}
	e.admission.Lock()
	defer e.admission.Unlock()
	e.lifeMu.Lock()
	stopping := e.closed
	e.lifeMu.Unlock()
	if stopping {
		return ErrUnavailable
	}
	if op.Err() != nil {
		return op.Err()
	}
	select {
	case e.reserved <- struct{}{}:
	default:
		return ErrQueueFull
	}
	saved := false
	defer func() {
		if !saved {
			<-e.reserved
		}
	}()
	if n, err := e.store.PendingCount(op, sub.UserID); err != nil {
		return err
	} else if n >= 3 {
		return ErrTooMany
	}
	sub.Status = "pending"
	if err = e.store.CreateSubmission(op, sub); err != nil {
		if errors.Is(err, store.ErrTooManyPending) {
			return ErrTooMany
		}
		return err
	}
	e.known[sub.ID] = true
	e.queue <- *sub // reservation guarantees this send cannot block
	saved = true
	return nil
}

func (e *Engine) recoverPending() {
	if e.store == nil || e.ctx.Err() != nil {
		return
	}
	e.admission.Lock()
	defer e.admission.Unlock()
	e.lifeMu.Lock()
	stopping := e.closed
	e.lifeMu.Unlock()
	if stopping {
		return
	}
	pending, err := e.store.PendingSubmissions(e.ctx)
	if err != nil {
		if e.ctx.Err() == nil {
			slog.Error("submission recovery failed")
		}
		return
	}
	for _, sub := range pending {
		if e.known[sub.ID] {
			continue
		}
		select {
		case e.reserved <- struct{}{}:
		default:
			return
		}
		e.known[sub.ID] = true
		e.queue <- sub
	}
}

func (e *Engine) worker() {
	defer e.wg.Done()
	drain := false
	for {
		if drain {
			select {
			case <-e.ctx.Done():
				return
			case sub := <-e.queue:
				<-e.reserved
				e.process(sub)
				e.admission.Lock()
				delete(e.known, sub.ID)
				e.admission.Unlock()
			default:
				return
			}
			continue
		}
		select {
		case <-e.ctx.Done():
			return
		case <-e.stopCh:
			drain = true
		case sub := <-e.queue:
			<-e.reserved
			e.process(sub)
			e.admission.Lock()
			delete(e.known, sub.ID)
			e.admission.Unlock()
		}
	}
}

func (e *Engine) process(sub model.Submission) {
	// Recovered submissions stay pending/judging while sandbox/toolchain is down.
	for {
		if e.ctx.Err() != nil {
			return
		}
		_, err := e.language(sub.Language)
		if err == nil {
			break
		}
		if errors.Is(err, ErrLanguage) {
			sub.Status = "system_error"
			sub.Message = "配置中不存在提交语言"
			e.finish(&sub)
			return
		}
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	p, err := e.store.GetProblem(e.ctx, sub.ProblemID)
	if err != nil {
		sub.Status = "system_error"
		sub.Message = "无法读取题目"
		e.finish(&sub)
		return
	}
	if err = e.store.MarkJudging(e.ctx, sub.ID); err != nil {
		return
	}
	result, err := e.Evaluate(e.ctx, p, sub.Language, sub.Code)
	if e.ctx.Err() != nil {
		return
	}
	if errors.Is(err, ErrUnavailable) {
		return
	} // maintenance will retry the durable row
	if err != nil {
		sub.Status = "system_error"
		sub.Message = "判题服务暂时异常"
	} else {
		sub.Status = result.Status
		sub.Message = result.Message
		sub.TimeUs = result.TimeUs
		sub.TimeMS = result.TimeMS
		sub.MemoryKB = result.MemoryKB
		sub.Passed = result.Passed
		sub.Total = result.Total
		sub.CaseResults = result.CaseResults
	}
	e.finish(&sub)
}

func (e *Engine) finish(sub *model.Submission) {
	sub.JudgedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := e.store.FinishSubmission(e.ctx, sub); err != nil && e.ctx.Err() == nil {
		slog.Error("persist judge result failed", "submission_id", sub.ID)
	}
}

func (e *Engine) validateCode(id, code string) (Language, error) {
	if code == "" || int64(len(code)) > e.config.MaxCodeBytes {
		return Language{}, ErrInvalidSubmission
	}
	return e.language(id)
}

func (e *Engine) compile(ctx context.Context, l Language, code, core string, limits model.Limits) (sandbox.CompileResult, error) {
	compileCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := e.sb.Compile(compileCtx, sandbox.CompileRequest{Language: l, Code: code, Limits: limits, CPUSet: core})
	if err != nil {
		if r.ArtifactID != "" {
			e.release(r.ArtifactID)
		}
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		if errors.Is(compileCtx.Err(), context.DeadlineExceeded) {
			r.Status = "compile_error"
			r.Stderr = "编译超过 10 秒限制"
			return r, nil
		}
		slog.Warn("sandbox compile failed", "language", l.ID, "error", err)
		e.markUnavailable(l.ID)
		return r, ErrUnavailable
	}
	if r.Status == "accepted" && r.ArtifactID == "" {
		r.Status = "system_error"
		r.Stderr = "编译产物缺失"
	}
	return r, nil
}

func (e *Engine) markUnavailable(id string) {
	e.healthMu.Lock()
	delete(e.available, id)
	e.healthMu.Unlock()
}

func (e *Engine) release(id string) {
	if id == "" {
		return
	}
	// Cleanup must outlive request cancellation, including Engine.Close.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.sb.Release(ctx, id); err != nil {
		if err = e.sb.Release(ctx, id); err != nil {
			slog.Warn("sandbox artifact cleanup failed")
		}
	}
}

func (e *Engine) runCase(ctx context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(request.Limits.TimeLimitMS*2+1000)*time.Millisecond+5*time.Second)
	defer cancel()
	r, err := e.sb.Run(runCtx, request)
	if err != nil {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		slog.Warn("sandbox run failed", "language", request.Language.ID, "error", err)
		e.markUnavailable(request.Language.ID)
		return r, ErrUnavailable
	}
	r.TimeMS = ceilMillis(r.TimeUs)
	if e.config.RerunBorderline && r.Status == "time_limit_exceeded" && r.TimeUs <= request.Limits.TimeLimitMS*1500 {
		// A second real sandbox result is required. In particular a low-CPU
		// sleeping timeout remains TLE unless the rerun actually succeeds.
		secondCtx, secondCancel := context.WithTimeout(ctx, time.Duration(request.Limits.TimeLimitMS*2+1000)*time.Millisecond+5*time.Second)
		second, secondErr := e.sb.Run(secondCtx, request)
		secondCancel()
		if secondErr != nil {
			if ctx.Err() != nil {
				return r, ctx.Err()
			}
			return r, nil
		}
		second.TimeMS = ceilMillis(second.TimeUs)
		if second.TimeUs < r.TimeUs || second.TimeUs == r.TimeUs && second.Status == "accepted" {
			r = second
		}
	}
	return r, nil
}

func (e *Engine) Evaluate(ctx context.Context, p *model.Problem, language, code string) (*model.Submission, error) {
	op, done, err := e.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	l, err := e.validateCode(language, code)
	if err != nil {
		return nil, err
	}
	if p == nil || len(p.Testcases) == 0 {
		return nil, ErrInvalidSubmission
	}
	core, err := e.acquire(op)
	if err != nil {
		return nil, err
	}
	defer func() { e.slots <- core }()
	limits := EffectiveLimits(p, l)
	result := &model.Submission{ProblemID: p.ID, ProblemTitle: p.Title, Language: language, Status: "accepted", Total: len(p.Testcases), CaseResults: []model.CaseResult{}}
	compiled, err := e.compile(op, l, code, core, limits)
	if err != nil {
		return nil, err
	}
	defer e.release(compiled.ArtifactID)
	if compiled.Status != "accepted" {
		result.Status = compiled.Status
		result.Message = compileMessage(compiled)
		return result, nil
	}
	for i, tc := range p.Testcases {
		r, err := e.runCase(op, sandbox.RunRequest{Language: l, ArtifactID: compiled.ArtifactID, Input: tc.Input, Limits: limits, CPUSet: core})
		if err != nil {
			return nil, err
		}
		status := r.Status
		if status == "accepted" && !CheckOutput(p.Checker, tc.Expected, r.Stdout, p.FloatEps) {
			status = "wrong_answer"
		}
		index := tc.Index
		if index == 0 {
			index = i + 1
		}
		cr := model.CaseResult{Index: index, Status: status, TimeUs: r.TimeUs, TimeMS: ceilMillis(r.TimeUs), MemoryKB: r.MemoryKB, IsSample: tc.IsSample}
		if tc.IsSample {
			cr.Input = truncate(tc.Input, 2048)
			cr.Expected = truncate(tc.Expected, 2048)
			cr.Actual = truncate(r.Stdout, 2048)
		}
		result.CaseResults = append(result.CaseResults, cr)
		result.TimeUs = max(result.TimeUs, r.TimeUs)
		result.TimeMS = ceilMillis(result.TimeUs)
		result.MemoryKB = max(result.MemoryKB, r.MemoryKB)
		if status == "accepted" {
			result.Passed++
			continue
		}
		result.Status = status
		result.Message = caseMessage(status, index)
		// Runtime diagnostics are useful for public samples, but hidden case
		// input must never be echoed through stderr.
		if tc.IsSample && r.Stderr != "" {
			if summary := sanitizeDiagnostics(r.Stderr, 1024); summary != "" {
				result.Message += "\n运行诊断：\n" + summary
			}
		}
		break
	}
	result.JudgedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}

func (e *Engine) Run(ctx context.Context, language, code, input string, p *model.Problem) (sandbox.RunResult, error) {
	op, done, err := e.begin(ctx)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	defer done()
	l, err := e.validateCode(language, code)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	if len(input) > 64<<10 {
		return sandbox.RunResult{}, ErrInvalidSubmission
	}
	core, err := e.acquire(op)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	defer func() { e.slots <- core }()
	limits := EffectiveLimits(p, l)
	compiled, err := e.compile(op, l, code, core, limits)
	if err != nil {
		return sandbox.RunResult{}, err
	}
	defer e.release(compiled.ArtifactID)
	if compiled.Status != "accepted" {
		return sandbox.RunResult{Status: compiled.Status, Stderr: compileMessage(compiled)}, nil
	}
	r, err := e.runCase(op, sandbox.RunRequest{Language: l, ArtifactID: compiled.ArtifactID, Input: input, Limits: limits, CPUSet: core})
	if err != nil {
		return r, err
	}
	if len(r.Stdout) > 65536 || len(r.Stderr) > 65536 {
		r.Truncated = true
	}
	r.Stdout = truncate(r.Stdout, 65536)
	r.Stderr = truncate(r.Stderr, 65536)
	return r, nil
}

func (e *Engine) Verify(ctx context.Context, p *model.Problem) (any, error) {
	if p == nil || len(p.Solutions) == 0 {
		return nil, ErrInvalidSubmission
	}
	results := make([]*model.Submission, 0, len(p.Solutions))
	for _, solution := range p.Solutions {
		r, err := e.Evaluate(ctx, p, solution.Language, solution.Code)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return struct {
		Results []*model.Submission `json:"results"`
	}{results}, nil
}

func ceilMillis(us int64) int64 {
	if us <= 0 {
		return 0
	}
	return 1 + (us-1)/1000
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return strings.ToValidUTF8(s, "�")
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

func compileMessage(r sandbox.CompileResult) string {
	if r.Status == "system_error" {
		return "编译服务异常"
	}
	s := r.Stderr
	if s == "" {
		s = r.Stdout
	}
	if s == "" {
		s = "编译失败"
	}
	return sanitizeDiagnostics(s, 4096)
}

var absolutePathFragment = regexp.MustCompile(`(^|[\s("'])/(?:[^:\s()"'\\]+/)+[^:\s()"']*`)

// Compiler output can contain container paths. Keep actionable diagnostics while
// replacing absolute filesystem paths with a stable placeholder.
func sanitizeDiagnostics(s string, limit int) string {
	s = absolutePathFragment.ReplaceAllString(s, `${1}[sandbox]/`)
	return truncate(s, limit)
}

func caseMessage(status string, index int) string {
	text := map[string]string{"wrong_answer": "答案错误", "runtime_error": "程序异常退出", "time_limit_exceeded": "超过时间限制", "memory_limit_exceeded": "超过内存限制", "output_limit_exceeded": "超过输出限制", "system_error": "判题服务异常"}[status]
	if text == "" {
		text = "程序执行失败"
	}
	return fmt.Sprintf("用例 %d：%s", index, text)
}
