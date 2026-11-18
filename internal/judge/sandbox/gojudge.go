package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"oj/internal/model"
)

const (
	OutputLimitBytes  = 1 << 20
	MaxCaseInputBytes = 1 << 20
	maxResponseBytes  = 16 << 20
	maxSourceBytes    = 4 << 20
)

// GoJudge implements https://docs.goj.ac/api. It never starts local processes.
type GoJudge struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewGoJudge(endpoint, token string) *GoJudge {
	return &GoJudge{baseURL: strings.TrimRight(endpoint, "/"), token: token,
		client: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type command struct {
	Args          []string       `json:"args"`
	Env           []string       `json:"env"`
	Files         []any          `json:"files"`
	CPULimit      int64          `json:"cpuLimit"`
	ClockLimit    int64          `json:"clockLimit"`
	MemoryLimit   int64          `json:"memoryLimit"`
	StackLimit    int64          `json:"stackLimit"`
	ProcLimit     int            `json:"procLimit"`
	CPUSet        string         `json:"cpuSetLimit,omitempty"`
	CopyIn        map[string]any `json:"copyIn,omitempty"`
	CopyOutCached []string       `json:"copyOutCached,omitempty"`
	CopyOutMax    int64          `json:"copyOutMax,omitempty"`
}

type result struct {
	Status     string            `json:"status"`
	Error      string            `json:"error"`
	ExitStatus int               `json:"exitStatus"`
	Time       int64             `json:"time"`
	RunTime    int64             `json:"runTime"`
	Memory     int64             `json:"memory"`
	Files      map[string]string `json:"files"`
	FileIDs    map[string]string `json:"fileIds"`
	FileErrors []struct {
		Type string `json:"type"`
	} `json:"fileError"`
}

// ProbeFailure records diagnostics for operator logs. Its public error string
// excludes output, environment values and sandbox paths.
type ProbeFailure struct {
	Status   string
	ExitCode int
	Detail   string
}

func (e *ProbeFailure) Error() string {
	return fmt.Sprintf("%s: probe status=%s exit_code=%d", ErrUnavailable, e.Status, e.ExitCode)
}

func (*ProbeFailure) Unwrap() error { return ErrUnavailable }

func validName(s string) bool {
	return s != "" && s != "." && !strings.ContainsAny(s, "\\\x00?\r\n") && !strings.HasPrefix(s, "/") && path.Clean(s) == s && s != ".." && !strings.HasPrefix(s, "../")
}

func validID(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func normalizeLimits(l model.Limits) (model.Limits, error) {
	if l.TimeLimitMS == 0 {
		l.TimeLimitMS = 1000
	}
	if l.MemoryLimitMB == 0 {
		l.MemoryLimitMB = 256
	}
	if l.TimeLimitMS < 1 || l.TimeLimitMS > 120000 || l.MemoryLimitMB < 1 || l.MemoryLimitMB > 4096 {
		return l, ErrInvalidRequest
	}
	return l, nil
}

func makeCommand(args []string, input string, l model.Limits, procs int, cpuset string) command {
	if procs < 1 {
		procs = 1
	}
	a := make([]string, len(args))
	for i, s := range args {
		a[i] = strings.ReplaceAll(s, "{memory_mb}", strconv.FormatInt(l.MemoryLimitMB, 10))
	}
	return command{Args: a, Env: []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "HOME=/w"},
		Files:    []any{map[string]string{"content": input}, map[string]any{"name": "stdout", "max": OutputLimitBytes, "pipe": true}, map[string]any{"name": "stderr", "max": OutputLimitBytes, "pipe": true}},
		CPULimit: l.TimeLimitMS * 1000000, ClockLimit: (l.TimeLimitMS*2 + 1000) * 1000000,
		MemoryLimit: l.MemoryLimitMB * 1024 * 1024, StackLimit: l.MemoryLimitMB * 1024 * 1024, ProcLimit: procs, CPUSet: cpuset}
}

func (g *GoJudge) request(ctx context.Context, method, route, contentType string, body io.Reader) ([]byte, error) {
	u, err := url.Parse(g.baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+route, body)
	if err != nil {
		return nil, ErrUnavailable
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable // Never expose URL, token, transport or backend diagnostics.
	}
	defer resp.Body.Close()
	if method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w (HTTP %d)", ErrUnavailable, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	if len(b) > maxResponseBytes {
		return nil, ErrUnavailable
	}
	return b, nil
}

func (g *GoJudge) execute(ctx context.Context, c command) (result, error) {
	b, err := json.Marshal(struct {
		Cmd []command `json:"cmd"`
	}{[]command{c}})
	if err != nil {
		return result{}, ErrInvalidRequest
	}
	b, err = g.request(ctx, http.MethodPost, "/run", "application/json", bytes.NewReader(b))
	if err != nil {
		return result{}, err
	}
	var results []result
	if json.Unmarshal(b, &results) != nil || len(results) != 1 {
		return result{}, ErrUnavailable
	}
	return results[0], nil
}

func (g *GoJudge) Probe(ctx context.Context, l Language) error {
	if len(l.Probe) == 0 {
		return ErrInvalidRequest
	}
	c := makeCommand(l.Probe, "", model.Limits{TimeLimitMS: 5000, MemoryLimitMB: 1024}, 128, "")
	r, err := g.execute(ctx, c)
	if err != nil {
		return err
	}
	if r.Status != "Accepted" || r.ExitStatus != 0 {
		converted := convert(r)
		detail := strings.TrimSpace(strings.Join([]string{r.Error, converted.Stderr, converted.Stdout}, "\n"))
		if g.token != "" {
			detail = strings.ReplaceAll(detail, g.token, "[redacted]")
		}
		if g.baseURL != "" {
			detail = strings.ReplaceAll(detail, g.baseURL, "[sandbox]")
		}
		return &ProbeFailure{Status: converted.Status, ExitCode: r.ExitStatus, Detail: detail}
	}
	return nil
}

func (g *GoJudge) Compile(ctx context.Context, r CompileRequest) (CompileResult, error) {
	fail := CompileResult{Status: "system_error"}
	if !validName(r.Language.Source) || !validName(r.Language.Artifact) || len(r.Code) > maxSourceBytes {
		return fail, ErrInvalidRequest
	}
	if len(r.Language.Compile) == 0 {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		f, err := w.CreateFormFile("file", r.Language.Artifact)
		if err != nil {
			return fail, ErrInvalidRequest
		}
		if _, err = io.WriteString(f, r.Code); err != nil {
			return fail, ErrInvalidRequest
		}
		if err = w.Close(); err != nil {
			return fail, ErrInvalidRequest
		}
		b, err := g.request(ctx, http.MethodPost, "/file", w.FormDataContentType(), &body)
		if err != nil {
			return fail, err
		}
		var id string
		if json.Unmarshal(b, &id) != nil || !validID(id) {
			return fail, ErrUnavailable
		}
		return CompileResult{ArtifactID: id, Status: "accepted"}, nil
	}
	// Compilation has a separate generous budget; solution limits apply to execution.
	c := makeCommand(r.Language.Compile, "", model.Limits{TimeLimitMS: 30000, MemoryLimitMB: 1024}, 128, r.CPUSet)
	c.CopyIn = map[string]any{r.Language.Source: map[string]string{"content": r.Code}}
	c.CopyOutCached = []string{r.Language.Artifact}
	c.CopyOutMax = 64 << 20
	raw, err := g.execute(ctx, c)
	if err != nil {
		return fail, err
	}
	run := convert(raw)
	out := CompileResult{Status: run.Status, Stdout: run.Stdout, Stderr: run.Stderr, TimeUs: run.TimeUs, TimeMS: run.TimeMS, MemoryKB: run.MemoryKB}
	id := raw.FileIDs[r.Language.Artifact]
	switch raw.Status {
	case "Accepted":
		if raw.ExitStatus == 0 && validID(id) {
			out.Status = "accepted"
			out.ArtifactID = id
		} else {
			out.Status = "system_error"
		}
	case "Non Zero Exit Status", "Nonzero Exit Status", "Signalled", "Dangerous Syscall":
		out.Status = "compile_error"
	case "Time Limit Exceeded":
		out.Status = "compile_error"
	default:
		// File Error, Internal Error and unknown backend states are systemic
		// failures rather than mistakes in the submitted source.
		out.Status = "system_error"
	}
	// Failed compiles can still cache an artifact; never leak it or unrelated files.
	g.cleanupExcept(raw.FileIDs, out.ArtifactID)
	if out.Status == "system_error" {
		if raw.Status == "Accepted" && raw.ExitStatus == 0 {
			return out, fmt.Errorf("%w: compiler did not return a valid artifact", ErrUnavailable)
		}
		return out, backendFailure("compile", raw)
	}
	return out, nil
}

func (g *GoJudge) cleanupExcept(ids map[string]string, keep string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	seen := make(map[string]bool)
	for _, id := range ids {
		if id != keep && !seen[id] && validID(id) {
			seen[id] = true
			_ = g.Release(ctx, id)
		}
	}
}

func (g *GoJudge) Run(ctx context.Context, r RunRequest) (RunResult, error) {
	fail := RunResult{Status: "system_error"}
	if len(r.Language.Run) == 0 || !validName(r.Language.Artifact) || !validID(r.ArtifactID) || len(r.Input) > MaxCaseInputBytes {
		return fail, ErrInvalidRequest
	}
	l, err := normalizeLimits(r.Limits)
	if err != nil {
		return fail, err
	}
	c := makeCommand(r.Language.Run, r.Input, l, r.Language.ProcLimit, r.CPUSet)
	c.CopyIn = map[string]any{r.Language.Artifact: map[string]string{"fileId": r.ArtifactID}}
	raw, err := g.execute(ctx, c)
	if err != nil {
		return fail, err
	}
	out := convert(raw)
	if out.Status == "system_error" {
		return out, backendFailure("run", raw)
	}
	return out, nil
}

func backendFailure(stage string, r result) error {
	// Status is reported by the backend. Use a fixed vocabulary in logs.
	status := "unexpected status"
	switch r.Status {
	case "Internal Error", "File Error", "Memory Limit Exceeded", "Output Limit Exceeded", "Accepted":
		status = r.Status
	}
	return fmt.Errorf("%w: %s result: %s (exit_code=%d)", ErrUnavailable, stage, status, r.ExitStatus)
}

func (g *GoJudge) Release(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	if !validID(id) {
		return ErrInvalidRequest
	}
	_, err := g.request(ctx, http.MethodDelete, "/file/"+url.PathEscape(id), "", nil)
	return err
}

func bounded(s string) (string, bool) {
	cut := len(s) > OutputLimitBytes
	if cut {
		s = s[:OutputLimitBytes]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s, cut
}

func nonnegative(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func convert(r result) RunResult {
	out := RunResult{Status: "system_error", ExitCode: r.ExitStatus, TimeUs: nonnegative(r.Time) / 1000, TimeMS: nonnegative(r.Time) / 1000000, WallTimeUs: nonnegative(r.RunTime) / 1000, MemoryKB: nonnegative(r.Memory) / 1024}
	switch r.Status {
	case "Accepted":
		out.Status = "accepted"
		if r.ExitStatus != 0 {
			out.Status = "runtime_error"
		}
	case "Time Limit Exceeded":
		out.Status = "time_limit_exceeded"
	case "Memory Limit Exceeded":
		out.Status = "memory_limit_exceeded"
	case "Output Limit Exceeded":
		out.Status = "output_limit_exceeded"
		out.Truncated = true
	case "Nonzero Exit Status", "Non Zero Exit Status", "Signalled", "Dangerous Syscall":
		out.Status = "runtime_error"
	}
	var a, b bool
	out.Stdout, a = bounded(r.Files["stdout"])
	out.Stderr, b = bounded(r.Files["stderr"])
	out.Truncated = out.Truncated || a || b
	for _, e := range r.FileErrors {
		if e.Type == "CollectSizeExceeded" || e.Type == "CopyOutSizeExceeded" {
			out.Truncated = true
			if out.Status == "accepted" || out.Status == "system_error" {
				out.Status = "output_limit_exceeded"
			}
		}
	}
	if (a || b) && out.Status == "accepted" {
		out.Status = "output_limit_exceeded"
	}
	return out
}

var _ Sandbox = (*GoJudge)(nil)
