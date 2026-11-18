package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"oj/internal/auth"
	"oj/internal/config"
	"oj/internal/judge"
	"oj/internal/model"
	"oj/internal/store"
	"oj/web"
)

type Server struct {
	store     *store.Store
	judge     *judge.Engine
	cfg       config.Config
	languages map[string]judge.Language
	policy    auth.Policy
	mu        sync.Mutex
	last      map[string]time.Time
}

func New(s *store.Store, a *auth.Service, j *judge.Engine, c config.Config) http.Handler {
	h := &Server{store: s, judge: j, cfg: c, last: map[string]time.Time{}}
	h.languages, _ = judge.LoadLanguagesOrDefault(c.LanguagesFile)
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		healthy := j.Healthy()
		status := "ok"
		if !healthy {
			status = "degraded"
		}
		write(w, 200, map[string]any{"status": status, "sandbox_available": healthy, "available_languages": len(j.Available())})
	})
	mux.HandleFunc("GET /api/languages", h.listLanguages)
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Tags(r.Context())
		if e != nil {
			internal(w)
			return
		}
		if v == nil {
			v = []string{}
		}
		write(w, 200, map[string]any{"items": v})
	})
	mux.HandleFunc("GET /api/problems", h.listProblems)
	mux.HandleFunc("GET /api/problems/{id}", h.problem)
	mux.HandleFunc("GET /api/admin/problems", h.listProblems)
	mux.HandleFunc("GET /api/admin/problems/{id}", h.problem)
	mux.HandleFunc("POST /api/admin/problems", h.saveProblem)
	mux.HandleFunc("PUT /api/admin/problems/{id}", h.saveProblem)
	mux.HandleFunc("DELETE /api/admin/problems/{id}", h.deleteProblem)
	mux.HandleFunc("POST /api/admin/problems/{id}/verify", h.verify)
	mux.HandleFunc("POST /api/problems/{id}/submit", h.submit)
	mux.HandleFunc("POST /api/run", h.run)
	mux.HandleFunc("GET /api/submissions", h.submissions)
	mux.HandleFunc("GET /api/submissions/{id}", h.submission)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "not_found", "接口不存在") })
	var assets fs.FS = web.Assets
	if c.Dev {
		assets = os.DirFS("web")
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") && r.URL.Path != "/api.js" {
			fail(w, 404, "not_found", "接口不存在")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "method_not_allowed", "不支持该请求方法")
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			http.Redirect(w, r, "/problems", 302)
			return
		}
		if strings.HasPrefix(p, "js/") || strings.HasPrefix(p, "css/") || strings.HasPrefix(p, "vendor/") || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css") || p == "favicon.svg" {
			http.FileServer(http.FS(assets)).ServeHTTP(w, r)
			return
		}
		b, e := fs.ReadFile(assets, "index.html")
		if e != nil {
			internal(w)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == "GET" {
			_, _ = w.Write(b)
		}
	})
	return h.middleware(a.Middleware(mux))
}

func (h *Server) listLanguages(w http.ResponseWriter, r *http.Request) {
	available := h.judge.Available()
	ready := make(map[string]judge.Language, len(available))
	for _, l := range available {
		ready[l.ID] = l
	}
	languages := available
	if r.URL.Query().Get("include_unavailable") == "1" {
		languages = make([]judge.Language, 0, len(h.languages))
		for _, l := range h.languages {
			if active, ok := ready[l.ID]; ok {
				l = active
			}
			languages = append(languages, l)
		}
		sort.Slice(languages, func(i, j int) bool { return languages[i].ID < languages[j].ID })
	}
	items := make([]map[string]any, 0, len(languages))
	for _, l := range languages {
		_, isAvailable := ready[l.ID]
		items = append(items, map[string]any{
			"id": l.ID, "display": l.Display, "highlight": l.Highlight, "template": l.Template,
			"time_factor": l.TimeFactor, "time_extra_ms": l.TimeExtraMS, "memory_factor": l.MemoryFactor,
			"memory_extra_mb": l.MemoryExtraMB, "proc_limit": l.ProcLimit, "available": isAvailable,
		})
	}
	write(w, 200, map[string]any{"languages": items})
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, msg string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func internal(w http.ResponseWriter) {
	fail(w, 500, "internal_error", "服务暂时异常，请稍后重试")
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "invalid_json", "请求内容无效或超过大小限制")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "invalid_json", "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}
func number(s string, fallback, min, max int) int {
	n, e := strconv.Atoi(s)
	if e != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
func idOf(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_id", "编号无效")
		return 0, false
	}
	return id, true
}
func (h *Server) allowed(w http.ResponseWriter, r *http.Request, action string, resource any) bool {
	u := auth.UserFromContext(r.Context())
	if h.policy.Can(u, action, resource) {
		return true
	}
	if u == nil {
		fail(w, 401, "unauthorized", "请先登录")
	} else {
		fail(w, 403, "forbidden", "没有权限")
	}
	return false
}
func (h *Server) admin(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/admin/") }
func (h *Server) getProblem(w http.ResponseWriter, r *http.Request, full bool) (*model.Problem, bool) {
	id, ok := idOf(w, r)
	if !ok {
		return nil, false
	}
	p, e := h.store.GetProblem(r.Context(), id)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			fail(w, 404, "not_found", "题目不存在")
		} else {
			internal(w)
		}
		return nil, false
	}
	if !full && !p.IsPublic {
		fail(w, 404, "not_found", "题目不存在")
		return nil, false
	}
	return p, true
}
func (h *Server) listProblems(w http.ResponseWriter, r *http.Request) {
	admin := h.admin(r)
	if admin && !h.allowed(w, r, "manage", nil) {
		return
	}
	q := r.URL.Query()
	if len([]rune(q.Get("q"))) > 200 || len([]rune(q.Get("tag"))) > 24 {
		fail(w, 400, "validation_error", "搜索关键词或标签过长")
		return
	}
	if d := q.Get("difficulty"); d != "" && d != "easy" && d != "medium" && d != "hard" {
		fail(w, 400, "validation_error", "难度筛选无效")
		return
	}
	page := number(q.Get("page"), 1, 1, 1000000)
	size := number(q.Get("page_size"), 20, 1, 100)
	uid := int64(0)
	if u := auth.UserFromContext(r.Context()); u != nil {
		uid = u.ID
	}
	items, total, e := h.store.ListProblems(r.Context(), store.ProblemFilter{Page: page, PageSize: size, Query: q.Get("q"), Difficulty: q.Get("difficulty"), Tag: q.Get("tag"), Admin: admin, UserID: uid})
	if e != nil {
		internal(w)
		return
	}
	if items == nil {
		items = []model.Problem{}
	}
	for i := range items {
		redactProblem(&items[i])
	}
	write(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}
func redactProblem(p *model.Problem) {
	samples := []model.Testcase{}
	for _, t := range p.Testcases {
		if t.IsSample {
			samples = append(samples, t)
		}
	}
	p.Testcases = samples
	p.Solutions = nil
}
func (h *Server) problem(w http.ResponseWriter, r *http.Request) {
	admin := h.admin(r)
	if admin && !h.allowed(w, r, "manage", nil) {
		return
	}
	p, ok := h.getProblem(w, r, admin)
	if !ok {
		return
	}
	if !admin {
		redactProblem(p)
	}
	p.EffectiveLimits = map[string]model.Limits{}
	for _, l := range h.judge.Available() {
		p.EffectiveLimits[l.ID] = judge.EffectiveLimits(p, l)
	}
	for _, l := range h.languages {
		if _, ok := p.EffectiveLimits[l.ID]; !ok {
			p.EffectiveLimits[l.ID] = judge.EffectiveLimits(p, l)
		}
	}
	write(w, 200, p)
}
func (h *Server) saveProblem(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "manage", nil) {
		return
	}
	var p model.Problem
	if !decode(w, r, &p) {
		return
	}
	if r.Method == "PUT" {
		old, ok := h.getProblem(w, r, true)
		if !ok {
			return
		}
		p.ID = old.ID
		p.CreatedBy = old.CreatedBy
	} else {
		p.ID = 0
		p.CreatedBy = auth.UserFromContext(r.Context()).ID
	}
	if msg := validateProblem(&p, h.cfg.MaxCodeBytes); msg != "" {
		fail(w, 400, "validation_error", msg)
		return
	}
	if len(h.languages) > 0 {
		for lang := range p.LangLimits {
			if _, ok := h.languages[lang]; !ok {
				fail(w, 400, "validation_error", "语言限制包含未知语言")
				return
			}
		}
		for lang := range p.Templates {
			if _, ok := h.languages[lang]; !ok {
				fail(w, 400, "validation_error", "模板包含未知语言")
				return
			}
		}
		for _, sol := range p.Solutions {
			if _, ok := h.languages[sol.Language]; !ok {
				fail(w, 400, "validation_error", "参考答案包含未知语言")
				return
			}
		}
	}
	if e := h.store.SaveProblem(r.Context(), &p); e != nil {
		internal(w)
		return
	}
	write(w, 200, p)
}
func (h *Server) deleteProblem(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "manage", nil) {
		return
	}
	p, ok := h.getProblem(w, r, true)
	if !ok {
		return
	}
	if e := h.store.DeleteProblem(r.Context(), p.ID); e != nil {
		internal(w)
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}
func (h *Server) verify(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "manage", nil) {
		return
	}
	p, ok := h.getProblem(w, r, true)
	if !ok {
		return
	}
	if len(p.Solutions) == 0 {
		fail(w, 400, "validation_error", "请先保存至少一份参考答案")
		return
	}
	if !h.rate(w, r, "verify") {
		return
	}
	res, e := h.judge.Verify(r.Context(), p)
	if e != nil {
		judgeError(w, e)
		return
	}
	write(w, 200, res)
}
func (h *Server) rate(w http.ResponseWriter, r *http.Request, action string) bool {
	key := fmt.Sprintf("%d:%s", auth.UserFromContext(r.Context()).ID, action)
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if now.Sub(h.last[key]) < 3*time.Second {
		w.Header().Set("Retry-After", "3")
		fail(w, 429, "rate_limited", "操作过于频繁，请 3 秒后重试")
		return false
	}
	h.last[key] = now
	if len(h.last) > 10000 {
		for k, t := range h.last {
			if now.Sub(t) > time.Minute {
				delete(h.last, k)
			}
		}
		for k := range h.last {
			if len(h.last) <= 8000 {
				break
			}
			delete(h.last, k)
		}
	}
	return true
}
func (h *Server) validCode(w http.ResponseWriter, lang, code string) bool {
	if len(strings.TrimSpace(code)) == 0 || int64(len(code)) > h.cfg.MaxCodeBytes {
		fail(w, 400, "validation_error", fmt.Sprintf("代码不能为空且不得超过 %d 字节", h.cfg.MaxCodeBytes))
		return false
	}
	if !h.judge.Healthy() {
		judgeError(w, judge.ErrUnavailable)
		return false
	}
	for _, l := range h.judge.Available() {
		if l.ID == lang {
			return true
		}
	}
	fail(w, 400, "invalid_language", "该语言不可用")
	return false
}
func (h *Server) submit(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "authenticated", nil) {
		return
	}
	p, ok := h.getProblem(w, r, false)
	if !ok {
		return
	}
	var in struct {
		Language string `json:"language"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) || !h.validCode(w, in.Language, in.Code) || !h.rate(w, r, "submit") {
		return
	}
	sub := &model.Submission{ProblemID: p.ID, UserID: auth.UserFromContext(r.Context()).ID, Language: in.Language, Code: in.Code, Status: "pending"}
	if e := h.judge.Submit(r.Context(), sub); e != nil {
		judgeError(w, e)
		return
	}
	write(w, 202, map[string]int64{"submission_id": sub.ID})
}
func (h *Server) run(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "authenticated", nil) {
		return
	}
	var in struct {
		Language  string `json:"language"`
		Code      string `json:"code"`
		Input     string `json:"input"`
		ProblemID int64  `json:"problem_id"`
	}
	if !decode(w, r, &in) || !h.validCode(w, in.Language, in.Code) {
		return
	}
	if len(in.Input) > 65536 {
		fail(w, 400, "validation_error", "自测输入不得超过 64KB")
		return
	}
	var p *model.Problem
	if in.ProblemID != 0 {
		var e error
		p, e = h.store.GetProblem(r.Context(), in.ProblemID)
		if e != nil || !p.IsPublic {
			fail(w, 404, "not_found", "题目不存在")
			return
		}
	}
	if !h.rate(w, r, "run") {
		return
	}
	res, e := h.judge.Run(r.Context(), in.Language, in.Code, in.Input, p)
	if e != nil {
		judgeError(w, e)
		return
	}
	write(w, 200, res)
}
func judgeError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, judge.ErrUnavailable):
		fail(w, 503, "judge_unavailable", "判题服务不可用")
	case errors.Is(e, judge.ErrQueueFull):
		fail(w, 503, "queue_full", "判题队列已满，请稍后再试")
	case errors.Is(e, judge.ErrTooMany):
		fail(w, 429, "too_many_pending", "每个用户最多同时排队 3 个提交")
	default:
		fail(w, 503, "judge_error", "判题服务暂时不可用，请稍后重试")
	}
}
func redactSubmission(s *model.Submission) {
	s.TimeMS = (s.TimeUs + 999) / 1000
	for i := range s.CaseResults {
		c := &s.CaseResults[i]
		c.TimeMS = (c.TimeUs + 999) / 1000
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
}
func truncate(s string, n int) string {
	if len(s) > n {
		return strings.ToValidUTF8(s[:n], "")
	}
	return s
}
func (h *Server) submissions(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "authenticated", nil) {
		return
	}
	q := r.URL.Query()
	page := number(q.Get("page"), 1, 1, 1000000)
	pid, pe := strconv.ParseInt(q.Get("problem_id"), 10, 64)
	if q.Get("problem_id") != "" && (pe != nil || pid < 1) {
		fail(w, 400, "validation_error", "题目筛选编号无效")
		return
	}
	if status := q.Get("status"); status != "" {
		valid := false
		for _, s := range []string{"pending", "judging", "accepted", "wrong_answer", "time_limit_exceeded", "memory_limit_exceeded", "runtime_error", "compile_error", "output_limit_exceeded", "system_error"} {
			if status == s {
				valid = true
			}
		}
		if !valid {
			fail(w, 400, "validation_error", "提交状态筛选无效")
			return
		}
	}
	items, total, e := h.store.ListSubmissions(r.Context(), store.SubmissionFilter{Page: page, PageSize: 20, UserID: auth.UserFromContext(r.Context()).ID, ProblemID: pid, Status: q.Get("status")})
	if e != nil {
		internal(w)
		return
	}
	if items == nil {
		items = []model.Submission{}
	}
	for i := range items {
		redactSubmission(&items[i])
	}
	write(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": 20})
}
func (h *Server) submission(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "authenticated", nil) {
		return
	}
	id, ok := idOf(w, r)
	if !ok {
		return
	}
	s, e := h.store.GetSubmission(r.Context(), id)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			fail(w, 404, "not_found", "提交记录不存在")
		} else {
			internal(w)
		}
		return
	}
	if !h.allowed(w, r, "view_submission", s) {
		return
	}
	redactSubmission(s)
	write(w, 200, s)
}
func (h *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		r.Body = http.MaxBytesReader(w, r.Body, 24<<20)
		defer func() {
			if recover() != nil {
				log.Printf("request=%s panic recovered", id)
				internal(w)
			}
			log.Printf("request=%s method=%s path=%s duration=%s", id, r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}()
		next.ServeHTTP(w, r)
	})
}
