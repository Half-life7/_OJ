package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"oj/internal/auth"
	"oj/internal/config"
	"oj/internal/judge"
	"oj/internal/judge/sandbox"
	"oj/internal/model"
	"oj/internal/store"
	"oj/web"
)

// Only the execution boundary is fake: authentication, persistence, admission,
// judge workers, output comparison, and HTTP middleware all run normally.
type apiSandbox struct {
	compiles atomic.Int64
	runs     atomic.Int64
}

var _ sandbox.Sandbox = (*apiSandbox)(nil)

func (*apiSandbox) Probe(context.Context, sandbox.Language) error { return nil }
func (s *apiSandbox) Compile(ctx context.Context, req sandbox.CompileRequest) (sandbox.CompileResult, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.CompileResult{}, err
	}
	s.compiles.Add(1)
	return sandbox.CompileResult{Status: "accepted", ArtifactID: "api-test-artifact"}, nil
}
func (s *apiSandbox) Run(ctx context.Context, req sandbox.RunRequest) (sandbox.RunResult, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.RunResult{}, err
	}
	s.runs.Add(1)
	return sandbox.RunResult{Status: "accepted", Stdout: "42\n", TimeUs: 1201, TimeMS: 2, MemoryKB: 1024}, nil
}
func (*apiSandbox) Release(context.Context, string) error { return nil }

type apiHarness struct {
	server  *httptest.Server
	store   *store.Store
	sandbox *apiSandbox
	cfg     config.Config
}

func newAPIHarness(t *testing.T) *apiHarness {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close test store: %v", err)
		}
	})
	cfg := config.Config{
		Workers: 1, AllowRegister: true, CookieSecure: "false", MaxCodeBytes: apiMaxCodeBytes,
		AdminUsername: "api_admin", AdminPassword: "Admin-test-password-42",
		ArgonMemory: config.DefaultArgonMemory, ArgonIterations: config.DefaultArgonIterations, ArgonParallelism: config.DefaultArgonParallelism,
	}
	a, err := auth.New(s, cfg)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	if err := a.EnsureAdmin(context.Background()); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	sb := &apiSandbox{}
	langs := map[string]judge.Language{"python3": {
		ID: "python3", Display: "Python 3", Source: "main.py", Run: []string{"python3", "main.py"},
		Artifact: "main.py", Probe: []string{"python3", "--version"}, Template: "print(42)\n",
		TimeFactor: 1, MemoryFactor: 1, ProcLimit: 16,
	}}
	engine := judge.New(s, sb, langs, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start judge engine: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := engine.Close(closeCtx); err != nil {
			t.Errorf("close judge engine: %v", err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for !engine.Healthy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !engine.Healthy() {
		t.Fatal("fake sandbox did not become available")
	}
	srv := httptest.NewServer(New(s, a, engine, cfg))
	t.Cleanup(srv.Close)
	return &apiHarness{server: srv, store: s, sandbox: sb, cfg: cfg}
}

func (h *apiHarness) client(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

type apiResponse struct {
	status int
	header http.Header
	body   []byte
}

func (h *apiHarness) raw(t *testing.T, client *http.Client, method, path string, body []byte, overrides map[string]string) apiResponse {
	t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "oj")
	req.Header.Set("Origin", h.server.URL)
	for name, value := range overrides {
		if value == "" {
			req.Header.Del(name)
		} else {
			req.Header.Set(name, value)
		}
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("%s %s returned non-JSON: %s", method, path, clippedAPI(data))
	}
	return apiResponse{status: res.StatusCode, header: res.Header, body: data}
}

func (h *apiHarness) request(t *testing.T, client *http.Client, method, path string, body any) apiResponse {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	return h.raw(t, client, method, path, data, nil)
}

func clippedAPI(body []byte) string {
	if len(body) > 1000 {
		return string(body[:1000]) + "..."
	}
	return string(body)
}

func requireAPIStatus(t *testing.T, res apiResponse, statuses ...int) {
	t.Helper()
	for _, status := range statuses {
		if res.status == status {
			return
		}
	}
	t.Fatalf("HTTP status = %d, want %v; body: %s", res.status, statuses, clippedAPI(res.body))
}

func decodeAPI[T any](t *testing.T, res apiResponse) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, clippedAPI(res.body))
	}
	return out
}

func requireAPIError(t *testing.T, res apiResponse, status int, code string) {
	t.Helper()
	requireAPIStatus(t, res, status)
	out := decodeAPI[struct {
		Error struct{ Code, Message string }
	}](t, res)
	if out.Error.Code == "" || out.Error.Message == "" || (code != "" && out.Error.Code != code) {
		t.Fatalf("invalid error envelope (want code %q): %s", code, clippedAPI(res.body))
	}
}

func (h *apiHarness) login(t *testing.T, username, password string) *http.Client {
	t.Helper()
	client := h.client(t)
	res := h.request(t, client, "POST", "/api/auth/login", map[string]string{"username": username, "password": password})
	requireAPIStatus(t, res, http.StatusOK)
	u := decodeAPI[struct{ User model.User }](t, res).User
	if u.ID == 0 || u.Username != username {
		t.Fatalf("unexpected login identity: %+v", u)
	}
	if bytes.Contains(res.body, []byte("password")) {
		t.Fatal("login response leaked password fields")
	}
	serverURL, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.Jar.Cookies(serverURL)) == 0 {
		t.Fatal("login did not establish a session cookie")
	}
	return client
}

func (h *apiHarness) admin(t *testing.T) *http.Client {
	t.Helper()
	return h.login(t, h.cfg.AdminUsername, h.cfg.AdminPassword)
}

func (h *apiHarness) user(t *testing.T, name string) *http.Client {
	t.Helper()
	client := h.client(t)
	res := h.request(t, client, "POST", "/api/auth/register", map[string]string{"username": name, "password": "User-test-password-42"})
	requireAPIStatus(t, res, http.StatusOK, http.StatusCreated)
	u := decodeAPI[struct{ User model.User }](t, res).User
	if u.ID == 0 || u.Username != name || u.Role != "user" {
		t.Fatalf("unexpected registered identity: %+v", u)
	}
	if bytes.Contains(res.body, []byte("password")) {
		t.Fatal("registration response leaked password fields")
	}
	me := h.request(t, client, "GET", "/api/auth/me", nil)
	requireAPIStatus(t, me, http.StatusOK)
	identity := decodeAPI[struct {
		User  model.User
		Stats map[string]any
	}](t, me)
	if identity.User.ID != u.ID || identity.Stats == nil {
		t.Fatalf("registration session or /me envelope missing: %s", me.body)
	}
	// Use a fresh jar to prove login, rather than relying on registration's cookie.
	return h.login(t, name, "User-test-password-42")
}

func (h *apiHarness) createProblem(t *testing.T, admin *http.Client, p model.Problem) model.Problem {
	t.Helper()
	res := h.request(t, admin, "POST", "/api/admin/problems", p)
	requireAPIStatus(t, res, http.StatusOK)
	created := decodeAPI[model.Problem](t, res)
	if created.ID == 0 || created.CreatedBy == 0 || created.Title != strings.TrimSpace(p.Title) {
		t.Fatalf("problem was not created correctly: %+v", created)
	}
	return created
}

type apiProblemList struct {
	Items    []model.Problem `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type apiSubmissionList struct {
	Items    []model.Submission `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

func requirePublicProblem(t *testing.T, p model.Problem, raw []byte) {
	t.Helper()
	if !p.IsPublic || len(p.Solutions) != 0 {
		t.Fatalf("public API exposed a private problem or solution: id=%d", p.ID)
	}
	for _, tc := range p.Testcases {
		if !tc.IsSample {
			t.Fatalf("problem %d exposed a hidden testcase", p.ID)
		}
	}
	for _, secret := range []string{"1000000 -999958", "123456 -123414", "private-case-explanation", "reference-answer-only"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("public response leaked %q", secret)
		}
	}
}

func TestAPIProblemManagementAuthorizationAndFilters(t *testing.T) {
	h := newAPIHarness(t)
	admin, user, anonymous := h.admin(t), h.user(t, "reader"), h.client(t)
	p := apiProblem()
	p.Title, p.Tags = "  A+B  ", []string{"math", " math ", "入门"}
	first := h.createProblem(t, admin, p)
	if len(first.Tags) != 2 || len(first.Testcases) != 3 {
		t.Fatalf("created problem did not normalize its data: %+v", first)
	}
	p = apiProblem()
	p.Title, p.Difficulty, p.Tags = "Graph paths", "medium", []string{"graph"}
	second := h.createProblem(t, admin, p)
	p = apiProblem()
	p.Title, p.IsPublic, p.Tags = "Private A+B", false, []string{"secret-tag"}
	private := h.createProblem(t, admin, p)

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/admin/problems", nil},
		{"GET", fmt.Sprintf("/api/admin/problems/%d", first.ID), nil},
		{"POST", "/api/admin/problems", apiProblem()},
		{"PUT", fmt.Sprintf("/api/admin/problems/%d", first.ID), apiProblem()},
		{"DELETE", fmt.Sprintf("/api/admin/problems/%d", first.ID), nil},
		{"POST", fmt.Sprintf("/api/admin/problems/%d/verify", first.ID), nil},
	} {
		t.Run("authorization "+tc.method+" "+tc.path, func(t *testing.T) {
			requireAPIError(t, h.request(t, anonymous, tc.method, tc.path, tc.body), 401, "unauthorized")
			requireAPIError(t, h.request(t, user, tc.method, tc.path, tc.body), 403, "forbidden")
		})
	}
	for _, tc := range []struct {
		query             string
		ids               []int64
		total, page, size int
	}{
		{"", []int64{first.ID, second.ID}, 2, 1, 20},
		{"?q=A%2BB", []int64{first.ID}, 1, 1, 20},
		{"?difficulty=medium", []int64{second.ID}, 1, 1, 20},
		{"?tag=math", []int64{first.ID}, 1, 1, 20},
		{"?q=A%2BB&difficulty=easy&tag=math", []int64{first.ID}, 1, 1, 20},
		{"?q=A%2BB&difficulty=hard", []int64{}, 0, 1, 20},
		{"?tag=secret-tag", []int64{}, 0, 1, 20},
		{"?admin=true&is_public=false", []int64{first.ID, second.ID}, 2, 1, 20},
		{"?page=999&page_size=1", []int64{}, 2, 999, 1},
		{"?page=-3&page_size=999", []int64{first.ID, second.ID}, 2, 1, 100},
		{"?page=invalid&page_size=invalid", []int64{first.ID, second.ID}, 2, 1, 20},
	} {
		t.Run("public filters "+tc.query, func(t *testing.T) {
			res := h.request(t, anonymous, "GET", "/api/problems"+tc.query, nil)
			requireAPIStatus(t, res, 200)
			list := decodeAPI[apiProblemList](t, res)
			if list.Items == nil || len(list.Items) != len(tc.ids) || list.Total != tc.total || list.Page != tc.page || list.PageSize != tc.size {
				t.Fatalf("unexpected list envelope: %s", clippedAPI(res.body))
			}
			seen := map[int64]bool{}
			for _, item := range list.Items {
				seen[item.ID] = true
				requirePublicProblem(t, item, res.body)
			}
			for _, id := range tc.ids {
				if !seen[id] {
					t.Errorf("missing problem %d", id)
				}
			}
		})
	}
	adminList := h.request(t, admin, "GET", "/api/admin/problems", nil)
	requireAPIStatus(t, adminList, 200)
	if list := decodeAPI[apiProblemList](t, adminList); list.Total != 3 {
		t.Fatalf("admin cannot list private problems: %s", adminList.body)
	}
	for _, client := range []*http.Client{anonymous, user, admin} {
		res := h.request(t, client, "GET", fmt.Sprintf("/api/problems/%d", first.ID), nil)
		requireAPIStatus(t, res, 200)
		detail := decodeAPI[model.Problem](t, res)
		requirePublicProblem(t, detail, res.body)
		if len(detail.Testcases) != 1 || detail.Testcases[0].Input != "20 22\n" {
			t.Fatal("public detail lost its sample")
		}
		if limits, ok := detail.EffectiveLimits["python3"]; !ok || limits.TimeLimitMS != 1000 || limits.MemoryLimitMB != 128 {
			t.Fatalf("missing effective language limits: %+v", detail.EffectiveLimits)
		}
		requireAPIError(t, h.request(t, client, "GET", fmt.Sprintf("/api/problems/%d", private.ID), nil), 404, "not_found")
	}
	full := h.request(t, admin, "GET", fmt.Sprintf("/api/admin/problems/%d", private.ID), nil)
	requireAPIStatus(t, full, 200)
	if detail := decodeAPI[model.Problem](t, full); len(detail.Testcases) != 3 || len(detail.Solutions) != 1 {
		t.Fatal("admin detail lost hidden tests or reference solution")
	}
	requireAPIError(t, h.request(t, user, "POST", fmt.Sprintf("/api/problems/%d/submit", private.ID), map[string]string{"language": "python3", "code": "print(42)"}), 404, "not_found")
	requireAPIError(t, h.request(t, user, "POST", "/api/run", map[string]any{"problem_id": private.ID, "language": "python3", "code": "print(42)"}), 404, "not_found")
	first.Title, first.IsPublic = "Updated A+B", false
	updated := h.request(t, admin, "PUT", fmt.Sprintf("/api/admin/problems/%d", first.ID), first)
	requireAPIStatus(t, updated, 200)
	if got := decodeAPI[model.Problem](t, updated); got.ID != first.ID || got.CreatedBy != first.CreatedBy || got.Title != first.Title {
		t.Fatal("update did not preserve problem identity")
	}
	requireAPIError(t, h.request(t, anonymous, "GET", fmt.Sprintf("/api/problems/%d", first.ID), nil), 404, "not_found")
}

func TestAPICSRFAndAnonymousProtection(t *testing.T) {
	h := newAPIHarness(t)
	admin, anonymous := h.admin(t), h.client(t)
	p := h.createProblem(t, admin, apiProblem())
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/auth/me", nil}, {"GET", "/api/submissions", nil}, {"GET", "/api/submissions/1", nil},
		{"POST", fmt.Sprintf("/api/problems/%d/submit", p.ID), map[string]string{"language": "python3", "code": "print(42)"}},
		{"POST", "/api/run", map[string]string{"language": "python3", "code": "print(42)"}},
	} {
		t.Run("anonymous "+tc.path, func(t *testing.T) {
			requireAPIError(t, h.request(t, anonymous, tc.method, tc.path, tc.body), 401, "unauthorized")
		})
	}
	for _, endpoint := range []string{"/api/auth/login", "/api/auth/register", "/api/admin/problems"} {
		for _, tc := range []struct {
			name    string
			headers map[string]string
		}{
			{"missing requested-with", map[string]string{"X-Requested-With": ""}},
			{"wrong requested-with", map[string]string{"X-Requested-With": "XMLHttpRequest"}},
			{"foreign origin", map[string]string{"Origin": "https://attacker.invalid"}},
			{"null origin", map[string]string{"Origin": "null"}},
		} {
			t.Run(endpoint+" "+tc.name, func(t *testing.T) {
				requireAPIError(t, h.raw(t, admin, "POST", endpoint, []byte(`{}`), tc.headers), 403, "")
			})
		}
	}
	// Non-browser callers may omit Origin, but must still supply the custom header.
	res := h.raw(t, h.client(t), "POST", "/api/auth/login", []byte(fmt.Sprintf(`{"username":%q,"password":%q}`, h.cfg.AdminUsername, h.cfg.AdminPassword)), map[string]string{"Origin": ""})
	requireAPIStatus(t, res, 200)
	list := h.request(t, admin, "GET", "/api/admin/problems", nil)
	requireAPIStatus(t, list, 200)
	if decodeAPI[apiProblemList](t, list).Total != 1 {
		t.Fatal("rejected CSRF request mutated problems")
	}
}

func TestAPIVerifyReferenceSolutions(t *testing.T) {
	h := newAPIHarness(t)
	admin := h.admin(t)
	problem := h.createProblem(t, admin, apiProblem())
	response := h.request(t, admin, "POST", fmt.Sprintf("/api/admin/problems/%d/verify", problem.ID), map[string]any{})
	requireAPIStatus(t, response, http.StatusOK)
	verified := decodeAPI[struct {
		Results []model.Submission `json:"results"`
	}](t, response)
	if len(verified.Results) != 1 {
		t.Fatalf("verify returned %d results: %s", len(verified.Results), clippedAPI(response.body))
	}
	result := verified.Results[0]
	if result.Language != "python3" || result.Status != "accepted" || result.Passed != 3 || result.Total != 3 {
		t.Fatalf("invalid verification result: %+v", result)
	}
}

func (h *apiHarness) waitAccepted(t *testing.T, client *http.Client, id int64) apiResponse {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		res := h.request(t, client, "GET", fmt.Sprintf("/api/submissions/%d", id), nil)
		requireAPIStatus(t, res, 200)
		sub := decodeAPI[model.Submission](t, res)
		if sub.Status == "accepted" {
			return res
		}
		if sub.Status != "pending" && sub.Status != "judging" {
			t.Fatalf("unexpected verdict: %s", res.body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("submission did not finish: %s", res.body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func requireRedactedCases(t *testing.T, sub model.Submission, raw []byte) {
	t.Helper()
	if len(sub.CaseResults) != 3 {
		t.Fatalf("want 3 case results, got %d", len(sub.CaseResults))
	}
	samples, hidden := 0, 0
	for _, result := range sub.CaseResults {
		if result.Status != "accepted" || result.TimeMS != (result.TimeUs+999)/1000 {
			t.Fatalf("invalid case result: %+v", result)
		}
		if result.IsSample {
			samples++
			if result.Input != "20 22\n" || result.Expected != "42\n" || result.Actual != "42\n" {
				t.Fatalf("sample result lost its I/O: %+v", result)
			}
		} else {
			hidden++
			if result.Input != "" || result.Expected != "" || result.Actual != "" {
				t.Fatal("hidden testcase I/O leaked")
			}
		}
	}
	if samples != 1 || hidden != 2 {
		t.Fatalf("incorrect case visibility: samples=%d hidden=%d", samples, hidden)
	}
	for _, secret := range []string{"1000000 -999958", "123456 -123414", "reference-answer-only"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("submission response leaked %q", secret)
		}
	}
}

func TestAPISubmissionJudgingOwnershipAndProblemDeletion(t *testing.T) {
	h := newAPIHarness(t)
	admin, owner, other, anonymous := h.admin(t), h.user(t, "submitter"), h.user(t, "other_user"), h.client(t)
	p := h.createProblem(t, admin, apiProblem())
	code := "print(42)\n"
	path := fmt.Sprintf("/api/problems/%d/submit", p.ID)
	request := map[string]string{"language": "python3", "code": code}
	res := h.request(t, owner, "POST", path, request)
	requireAPIStatus(t, res, 202)
	id := decodeAPI[struct {
		ID int64 `json:"submission_id"`
	}](t, res).ID
	if id <= 0 {
		t.Fatalf("missing submission ID: %s", res.body)
	}
	limited := h.request(t, owner, "POST", path, request)
	requireAPIError(t, limited, 429, "rate_limited")
	if limited.header.Get("Retry-After") != "3" {
		t.Fatal("submission rate limit omitted retry advice")
	}
	accepted := h.waitAccepted(t, owner, id)
	sub := decodeAPI[model.Submission](t, accepted)
	if sub.ID != id || sub.ProblemID != p.ID || sub.Code != code || sub.Passed != 3 || sub.Total != 3 || sub.TimeUs <= 0 || sub.TimeMS != (sub.TimeUs+999)/1000 || sub.JudgedAt == "" {
		t.Fatalf("invalid judged submission: %+v", sub)
	}
	requireRedactedCases(t, sub, accepted.body)
	// Check the wire representation too: hidden I/O fields should be omitted.
	wire := decodeAPI[struct {
		Cases []map[string]json.RawMessage `json:"case_results"`
	}](t, accepted)
	for _, result := range wire.Cases {
		var sample bool
		if err := json.Unmarshal(result["is_sample"], &sample); err != nil {
			t.Fatal(err)
		}
		if !sample {
			for _, field := range []string{"input", "expected", "actual"} {
				if _, present := result[field]; present {
					t.Errorf("hidden case serialized %q", field)
				}
			}
		}
	}
	if h.sandbox.compiles.Load() < 1 || h.sandbox.runs.Load() != 3 {
		t.Fatalf("judge did not execute all cases: compiles=%d runs=%d", h.sandbox.compiles.Load(), h.sandbox.runs.Load())
	}
	detailPath := fmt.Sprintf("/api/submissions/%d", id)
	requireAPIError(t, h.request(t, other, "GET", detailPath, nil), 403, "forbidden")
	requireAPIError(t, h.request(t, anonymous, "GET", detailPath, nil), 401, "unauthorized")
	adminDetail := h.request(t, admin, "GET", detailPath, nil)
	requireAPIStatus(t, adminDetail, 200)
	requireRedactedCases(t, decodeAPI[model.Submission](t, adminDetail), adminDetail.body)
	for _, tc := range []struct {
		client *http.Client
		path   string
		total  int
	}{
		{owner, "/api/submissions", 1},
		{owner, fmt.Sprintf("/api/submissions?problem_id=%d&status=accepted", p.ID), 1},
		{owner, "/api/submissions?status=wrong_answer", 0},
		{owner, "/api/submissions?problem_id=999999", 0},
		{other, fmt.Sprintf("/api/submissions?user_id=%d", sub.UserID), 0},
	} {
		listRes := h.request(t, tc.client, "GET", tc.path, nil)
		requireAPIStatus(t, listRes, 200)
		list := decodeAPI[apiSubmissionList](t, listRes)
		if list.Items == nil || list.Total != tc.total || len(list.Items) != tc.total || list.Page != 1 || list.PageSize != 20 {
			t.Fatalf("unexpected history: %s", listRes.body)
		}
		for _, item := range list.Items {
			if item.ID != id || item.UserID != sub.UserID {
				t.Fatal("history exposed another user's submission")
			}
			requireRedactedCases(t, item, listRes.body)
		}
	}
	deleted := h.request(t, admin, "DELETE", fmt.Sprintf("/api/admin/problems/%d", p.ID), nil)
	requireAPIStatus(t, deleted, 200)
	if !decodeAPI[struct {
		OK bool `json:"ok"`
	}](t, deleted).OK {
		t.Fatal("delete did not acknowledge success")
	}
	requireAPIError(t, h.request(t, anonymous, "GET", fmt.Sprintf("/api/problems/%d", p.ID), nil), 404, "not_found")
	for _, client := range []*http.Client{owner, admin} {
		retained := h.request(t, client, "GET", detailPath, nil)
		requireAPIStatus(t, retained, 200)
		got := decodeAPI[model.Submission](t, retained)
		if got.ID != id || got.ProblemID != p.ID || !strings.HasPrefix(got.ProblemTitle, p.Title) || got.Status != "accepted" || got.Code != code {
			t.Fatalf("deletion damaged submission history: %+v", got)
		}
		requireRedactedCases(t, got, retained.body)
	}
	retainedList := h.request(t, owner, "GET", "/api/submissions", nil)
	requireAPIStatus(t, retainedList, 200)
	if list := decodeAPI[apiSubmissionList](t, retainedList); list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != id {
		t.Fatal("deleting problem removed submission from history")
	}
	requireAPIError(t, h.request(t, other, "GET", detailPath, nil), 403, "forbidden")
}

func TestAPIProblemValidationRejectsWithoutSaving(t *testing.T) {
	h := newAPIHarness(t)
	admin := h.admin(t)
	for _, tc := range invalidProblemCases() {
		t.Run(tc.name, func(t *testing.T) {
			p := apiProblem()
			tc.mutate(&p)
			requireAPIError(t, h.request(t, admin, "POST", "/api/admin/problems", p), 400, "validation_error")
		})
	}
	res := h.request(t, admin, "GET", "/api/admin/problems", nil)
	requireAPIStatus(t, res, 200)
	if list := decodeAPI[apiProblemList](t, res); list.Total != 0 || len(list.Items) != 0 {
		t.Fatal("rejected problem persisted")
	}
	p := h.createProblem(t, admin, apiProblem())
	p.TimeLimitMS = 0
	requireAPIError(t, h.request(t, admin, "PUT", fmt.Sprintf("/api/admin/problems/%d", p.ID), p), 400, "validation_error")
	saved := h.request(t, admin, "GET", fmt.Sprintf("/api/admin/problems/%d", p.ID), nil)
	requireAPIStatus(t, saved, 200)
	if decodeAPI[model.Problem](t, saved).TimeLimitMS != 1000 {
		t.Fatal("rejected update changed existing problem")
	}
}

func TestAPIRequestBodyAndExecutionLimits(t *testing.T) {
	h := newAPIHarness(t)
	admin, user := h.admin(t), h.user(t, "limit_user")
	p := h.createProblem(t, admin, apiProblem())
	submitPath := fmt.Sprintf("/api/problems/%d/submit", p.ID)
	for _, endpoint := range []struct {
		method, path string
		client       *http.Client
	}{
		{"POST", "/api/admin/problems", admin}, {"PUT", fmt.Sprintf("/api/admin/problems/%d", p.ID), admin},
		{"POST", submitPath, user}, {"POST", "/api/run", user},
	} {
		for name, body := range map[string]string{"empty": "", "malformed": "{", "array": "[]", "string": `"text"`, "multiple objects": "{} {}", "trailing scalar": "{} true", "trailing garbage": "{} x"} {
			t.Run(endpoint.method+" "+endpoint.path+" "+name, func(t *testing.T) {
				requireAPIError(t, h.raw(t, endpoint.client, endpoint.method, endpoint.path, []byte(body), nil), 400, "invalid_json")
			})
		}
	}
	for _, path := range []string{submitPath, "/api/run"} {
		for _, tc := range []struct{ name, language, code, errorCode string }{
			{"missing code", "python3", "", "validation_error"},
			{"blank code", "python3", " \r\n\t", "validation_error"},
			{"too much code", "python3", strings.Repeat("x", int(apiMaxCodeBytes)+1), "validation_error"},
			{"code counted in bytes", "python3", strings.Repeat("字", int(apiMaxCodeBytes)/3+1), "validation_error"},
			{"unknown language", "unknown", "print(42)", "invalid_language"},
			{"missing language", "", "print(42)", "invalid_language"},
		} {
			t.Run(path+" "+tc.name, func(t *testing.T) {
				requireAPIError(t, h.request(t, user, "POST", path, map[string]string{"language": tc.language, "code": tc.code}), 400, tc.errorCode)
			})
		}
	}
	requireAPIError(t, h.request(t, user, "POST", "/api/run", map[string]string{"language": "python3", "code": "print(42)", "input": strings.Repeat("x", 65537)}), 400, "validation_error")
	requireAPIError(t, h.request(t, user, "POST", "/api/run", map[string]any{"language": "python3", "code": "print(42)", "problem_id": 999999}), 404, "not_found")
	oversized := []byte(`{"padding":"` + strings.Repeat("x", 24<<20) + `"}`)
	requireAPIError(t, h.raw(t, admin, "POST", "/api/admin/problems", oversized, nil), 400, "invalid_json")
	if h.sandbox.runs.Load() != 0 || h.sandbox.compiles.Load() != 0 {
		t.Fatal("invalid request reached sandbox")
	}
	valid := map[string]any{"language": "python3", "code": strings.Repeat("x", int(apiMaxCodeBytes)), "input": strings.Repeat("x", 65536), "problem_id": p.ID}
	run := h.request(t, user, "POST", "/api/run", valid)
	requireAPIStatus(t, run, 200)
	result := decodeAPI[sandbox.RunResult](t, run)
	if result.Status != "accepted" || result.Stdout != "42\n" || result.TimeUs != 1201 {
		t.Fatalf("unexpected self-test result: %+v", result)
	}
	requireAPIError(t, h.request(t, user, "POST", "/api/run", valid), 429, "rate_limited")
	history := h.request(t, user, "GET", "/api/submissions", nil)
	requireAPIStatus(t, history, 200)
	if decodeAPI[apiSubmissionList](t, history).Total != 0 {
		t.Fatal("self-test or rejected submit created submission history")
	}
	for _, badID := range []string{"0", "-1", "nope", "9223372036854775808"} {
		for _, prefix := range []string{"/api/problems/", "/api/submissions/", "/api/admin/problems/"} {
			requireAPIError(t, h.request(t, admin, "GET", prefix+badID, nil), 400, "invalid_id")
		}
	}
	for _, path := range []string{"/api/problems/999999", "/api/submissions/999999", "/api/admin/problems/999999", "/api/does-not-exist"} {
		requireAPIError(t, h.request(t, admin, "GET", path, nil), 404, "not_found")
	}
}

func TestAPIStaticAssetsAndDeepLinkFallback(t *testing.T) {
	h := newAPIHarness(t)
	client := h.client(t)
	langsResponse := h.request(t, client, "GET", "/api/languages", nil)
	requireAPIStatus(t, langsResponse, 200)
	languages := decodeAPI[struct {
		Languages []map[string]json.RawMessage `json:"languages"`
	}](t, langsResponse).Languages
	if len(languages) != 1 {
		t.Fatalf("expected one available language: %s", langsResponse.body)
	}
	var languageID string
	if err := json.Unmarshal(languages[0]["id"], &languageID); err != nil || languageID != "python3" {
		t.Fatalf("unexpected available language: %s", langsResponse.body)
	}
	for _, field := range []string{"compile", "run", "probe", "source", "artifact"} {
		if _, present := languages[0][field]; present {
			t.Errorf("public language metadata exposed %q", field)
		}
	}
	configured := h.request(t, client, "GET", "/api/languages?include_unavailable=1", nil)
	requireAPIStatus(t, configured, 200)
	all := decodeAPI[struct {
		Languages []struct {
			ID        string `json:"id"`
			Template  string `json:"template"`
			Available bool   `json:"available"`
		} `json:"languages"`
	}](t, configured).Languages
	if len(all) != 3 || all[0].ID != "cpp17" || all[0].Available || all[0].Template == "" ||
		all[1].ID != "java" || all[1].Available || all[2].ID != "python3" || !all[2].Available {
		t.Fatalf("unexpected editor languages: %s", configured.body)
	}
	index, err := web.Assets.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	titleStart, titleEnd := bytes.Index(index, []byte("<title>")), bytes.Index(index, []byte("</title>"))
	if titleStart < 0 || titleEnd <= titleStart+len("<title>") {
		t.Fatal("application shell has no title")
	}
	for _, tc := range []struct {
		path, mediaType string
		want            []byte
	}{
		{"/api.js", "javascript", nil},
		{"/problems/123", "text/html", index},
		{"/submissions/456", "text/html", index},
		{"/admin/problems/new", "text/html", index},
	} {
		t.Run(tc.path, func(t *testing.T) {
			res, err := client.Get(h.server.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 200 {
				t.Fatalf("HTTP status = %d; body: %s", res.StatusCode, clippedAPI(body))
			}
			mediaType, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.mediaType == "javascript" {
				if mediaType != "text/javascript" && mediaType != "application/javascript" {
					t.Fatalf("JavaScript served as %q", mediaType)
				}
				want, err := web.Assets.ReadFile("api.js")
				if err != nil {
					t.Fatal(err)
				}
				if len(body) == 0 || !bytes.Equal(body, want) {
					t.Fatal("static asset did not serve the embedded JavaScript")
				}
			} else {
				if mediaType != tc.mediaType || !bytes.Equal(body, tc.want) {
					t.Fatalf("deep link did not serve the titled application shell; content type = %q", mediaType)
				}
				if res.Header.Get("Cache-Control") != "no-cache" {
					t.Fatal("application shell should not be cached")
				}
			}
		})
	}
	for _, path := range []string{"/api/unknown", "/apiunknown"} {
		t.Run(path, func(t *testing.T) { requireAPIError(t, h.request(t, client, "GET", path, nil), 404, "not_found") })
	}
}

func TestAPIRejectsInvalidListFilters(t *testing.T) {
	h := newAPIHarness(t)
	admin := h.admin(t)
	for _, prefix := range []string{"/api/problems", "/api/admin/problems"} {
		for _, query := range []string{
			"?difficulty=impossible",
			"?q=" + url.QueryEscape(strings.Repeat("题", 201)),
			"?tag=" + url.QueryEscape(strings.Repeat("标", 25)),
		} {
			t.Run(prefix+query[:min(len(query), 35)], func(t *testing.T) {
				requireAPIError(t, h.request(t, admin, "GET", prefix+query, nil), 400, "validation_error")
			})
		}
	}
	for _, query := range []string{"?problem_id=0", "?problem_id=-1", "?problem_id=not-a-number", "?problem_id=9223372036854775808", "?status=unknown", "?status=AC"} {
		t.Run(query, func(t *testing.T) {
			requireAPIError(t, h.request(t, admin, "GET", "/api/submissions"+query, nil), 400, "validation_error")
		})
	}
}

func TestRedactSubmissionHidesPrivateCasesAndBoundsSamples(t *testing.T) {
	sub := model.Submission{TimeUs: 1001, CaseResults: []model.CaseResult{
		{Index: 1, IsSample: true, TimeUs: 1, Input: strings.Repeat("字", 1000), Expected: strings.Repeat("e", 3000), Actual: strings.Repeat("a", 3000)},
		{Index: 2, TimeUs: 1001, Input: "secret input", Expected: "secret expected", Actual: "secret actual"},
	}}
	redactSubmission(&sub)
	if sub.TimeMS != 2 || sub.CaseResults[0].TimeMS != 1 || sub.CaseResults[1].TimeMS != 2 {
		t.Fatalf("microseconds were not rounded up: %+v", sub)
	}
	sample := sub.CaseResults[0]
	for _, value := range []string{sample.Input, sample.Expected, sample.Actual} {
		if value == "" || len(value) > 2048 || !utf8.ValidString(value) {
			t.Fatalf("sample output is not bounded valid UTF-8: %d bytes", len(value))
		}
	}
	hidden := sub.CaseResults[1]
	if hidden.Input != "" || hidden.Expected != "" || hidden.Actual != "" {
		t.Fatal("private case data survived redaction")
	}
}
