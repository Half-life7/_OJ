package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"oj/internal/config"
	"oj/internal/model"
	"oj/internal/store"
)

type harness struct {
	s       *Service
	handler http.Handler
	db      *store.Store
}

func newHarness(t *testing.T, customize ...func(*config.Config)) *harness {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := config.Config{AllowRegister: true, AdminUsername: "admin", AdminPassword: "admin-password", CookieSecure: "auto"}
	for _, f := range customize {
		f(&c)
	}
	s, err := New(db, c)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	mux.HandleFunc("POST /api/admin/problems", func(w http.ResponseWriter, r *http.Request) {
		u := authenticated(w, r)
		if u == nil {
			return
		}
		if !(Policy{}).Can(u, "manage", nil) {
			fail(w, 403, "forbidden", "没有权限")
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	return &harness{s: s, db: db, handler: s.Middleware(mux)}
}
func (h *harness) request(method, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://oj.test"+path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.10:1234"
	r.Header.Set("User-Agent", "auth-test")
	r.Header.Set("X-Requested-With", "oj")
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	for k, v := range headers {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w
}
func checkStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("HTTP %d want %d: %s", w.Code, want, w.Body.String())
	}
}
func cookieOf(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			return c
		}
	}
	t.Fatal("session cookie missing")
	return nil
}
func userOf(t *testing.T, w *httptest.ResponseRecorder) *model.User {
	t.Helper()
	var out struct {
		User model.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return &out.User
}
func (h *harness) register(t *testing.T, name string) (*model.User, *http.Cookie) {
	t.Helper()
	w := h.request("POST", "/api/auth/register", fmt.Sprintf(`{"username":%q,"password":"password-123"}`, name), nil, nil)
	checkStatus(t, w, 201)
	return userOf(t, w), cookieOf(t, w)
}
func (h *harness) login(t *testing.T, name, password string) *http.Cookie {
	t.Helper()
	w := h.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":%q,"password":%q}`, name, password), nil, nil)
	checkStatus(t, w, 200)
	return cookieOf(t, w)
}
func (h *harness) administrator(t *testing.T) (*model.User, *http.Cookie) {
	t.Helper()
	if err := h.s.EnsureAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := h.login(t, "admin", "admin-password")
	w := h.request("GET", "/api/auth/me", "", c, nil)
	checkStatus(t, w, 200)
	return userOf(t, w), c
}

func TestRegistrationLoginLogoutAndSessionStorage(t *testing.T) {
	h := newHarness(t)
	u, c := h.register(t, "Alice_1")
	if u.Role != "user" || u.Status != "active" || u.ID == 0 || u.PasswordHash != "" {
		t.Fatal("unsafe registered user")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.Path != "/" || c.MaxAge != 30*24*3600 || len(c.Value) != 43 {
		t.Fatalf("unsafe cookie flags: %+v", *c)
	}
	var hash, ua, ip string
	var expires int64
	if err := h.db.DB.QueryRow("SELECT token_hash,user_agent,ip,expires_at FROM sessions WHERE user_id=?", u.ID).Scan(&hash, &ua, &ip, &expires); err != nil {
		t.Fatal(err)
	}
	if hash != tokenHash(c.Value) || hash == c.Value || ua != "auth-test" || ip != "192.0.2.10" || expires < h.s.clock().Add(sessionLifetime-time.Minute).Unix() {
		t.Fatal("invalid persisted session")
	}
	w := h.request("GET", "/api/auth/me", "", c, nil)
	checkStatus(t, w, 200)
	for _, secret := range []string{"password_hash", "argon2id", "password-123", c.Value} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("secret leaked in me")
		}
	}
	if !strings.Contains(w.Body.String(), `"stats"`) {
		t.Fatal("stats missing")
	}
	checkStatus(t, h.request("POST", "/api/auth/register", `{"username":"aLiCe_1","password":"password-123"}`, nil, nil), 409)
	checkStatus(t, h.request("POST", "/api/auth/logout", `{}`, c, nil), 200)
	checkStatus(t, h.request("GET", "/api/auth/me", "", c, nil), 401)
	c = h.login(t, "ALICE_1", "password-123")
	w = h.request("POST", "/api/auth/login", `{"username":"alice_1","password":"password-123"}`, c, nil)
	checkStatus(t, w, 200)
	newCookie := cookieOf(t, w)
	if newCookie.Value == c.Value {
		t.Fatal("session fixation")
	}
	checkStatus(t, h.request("GET", "/api/auth/me", "", c, nil), 401)
	checkStatus(t, h.request("GET", "/api/auth/me", "", newCookie, nil), 200)
	if _, err := h.db.DB.Exec("UPDATE sessions SET expires_at=0"); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, h.request("GET", "/api/auth/me", "", newCookie, nil), 401)
	checkStatus(t, h.request("GET", "/api/auth/me", "", &http.Cookie{Name: sessionCookie, Value: "invalid"}, nil), 401)
}

func TestRegistrationValidationAndClosedMode(t *testing.T) {
	h := newHarness(t)
	cases := []string{`{"username":"ab","password":"password-123"}`, `{"username":"bad-name","password":"password-123"}`, `{"username":"中文名","password":"password-123"}`, `{"username":"alice","password":"short"}`, `{"username":"alice","password":"password-123","role":"admin"}`, `{"username":"alice","password":"password-123"} {}`, `null`, strings.Repeat("x", 9000)}
	for i, body := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) { checkStatus(t, h.request("POST", "/api/auth/register", body, nil, nil), 400) })
	}
	h.s.cfg.AllowRegister = false
	checkStatus(t, h.request("POST", "/api/auth/register", `{"username":"alice","password":"password-123"}`, nil, nil), 403)
	w := h.request("GET", "/api/auth/config", "", nil, nil)
	checkStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"allow_register":false`) {
		t.Fatal("wrong config")
	}
}

func TestLoginThrottleFifthFailureAndExpiry(t *testing.T) {
	h := newHarness(t)
	h.register(t, "alice")
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.s.clock = func() time.Time { return clock }
	wrong := `{"username":"alice","password":"incorrect-password"}`
	unknown := `{"username":"unknown","password":"incorrect-password"}`
	a := h.request("POST", "/api/auth/login", wrong, nil, nil)
	b := h.request("POST", "/api/auth/login", unknown, nil, nil)
	checkStatus(t, a, 401)
	checkStatus(t, b, 401)
	if a.Body.String() != b.Body.String() {
		t.Fatal("login leaks existence")
	}
	for i := 2; i <= 5; i++ {
		want := 401
		if i == 5 {
			want = 429
		}
		w := h.request("POST", "/api/auth/login", wrong, nil, map[string]string{"X-Forwarded-For": fmt.Sprintf("203.0.113.%d", i)})
		checkStatus(t, w, want)
	}
	checkStatus(t, h.request("POST", "/api/auth/login", `{"username":"ALICE","password":"password-123"}`, nil, nil), 429)
	clock = clock.Add(10 * time.Minute)
	h.login(t, "alice", "password-123")
	for i := 1; i <= 5; i++ {
		want := 401
		if i == 5 {
			want = 429
		}
		checkStatus(t, h.request("POST", "/api/auth/login", `{"username":"alice","password":"x"}`, nil, nil), want)
	}
}

func TestCSRFAllMutationsAndDevPreflight(t *testing.T) {
	h := newHarness(t)
	_, c := h.register(t, "alice")
	for _, path := range []string{"/api/auth/login", "/api/auth/register", "/api/auth/logout", "/api/auth/password", "/api/admin/problems"} {
		checkStatus(t, h.request("POST", path, `{}`, c, map[string]string{"X-Requested-With": ""}), 403)
	}
	for _, origin := range []string{"https://evil.test", "http://evil.test", "null", "http://oj.test.evil.test", "http://oj.test/path", "https://oj.test", "http://user@oj.test"} {
		checkStatus(t, h.request("POST", "/api/auth/logout", `{}`, c, map[string]string{"Origin": origin}), 403)
	}
	checkStatus(t, h.request("POST", "/api/auth/logout", `{}`, c, map[string]string{"Origin": "http://oj.test"}), 200)
	h.s.cfg.Dev = true
	w := h.request("OPTIONS", "/api/auth/login", "", nil, map[string]string{"Origin": "http://oj.test", "X-Requested-With": "", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "X-Requested-With, Content-Type"})
	checkStatus(t, w, 204)
	if w.Header().Get("Access-Control-Allow-Origin") != "http://oj.test" || w.Header().Get("Access-Control-Allow-Credentials") != "true" || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "X-Requested-With") {
		t.Fatal("preflight headers missing")
	}
	w = h.request("OPTIONS", "/api/auth/login", "", nil, map[string]string{"Origin": "http://localhost:5173", "X-Requested-With": "", "Access-Control-Request-Method": "POST"})
	checkStatus(t, w, 204)
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("development loopback origin was not allowed")
	}
	checkStatus(t, h.request("OPTIONS", "/api/auth/login", "", nil, map[string]string{"Origin": "http://evil.test", "X-Requested-With": ""}), 403)
	checkStatus(t, h.request("POST", "/api/auth/logout", `{}`, nil, map[string]string{"Origin": "http://localhost:5173"}), 200)
	checkStatus(t, h.request("POST", "/api/auth/logout", `{}`, nil, map[string]string{"Origin": "http://oj.test", "X-Requested-With": ""}), 403)
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	h := newHarness(t)
	_, first := h.register(t, "alice")
	second := h.login(t, "alice", "password-123")
	checkStatus(t, h.request("POST", "/api/auth/password", `{"old_password":"wrong-password","new_password":"new-password"}`, first, nil), 400)
	checkStatus(t, h.request("POST", "/api/auth/password", `{"old_password":"password-123","new_password":"short"}`, first, nil), 400)
	checkStatus(t, h.request("POST", "/api/auth/password", `{"old_password":"password-123","new_password":"new-password"}`, first, nil), 200)
	checkStatus(t, h.request("GET", "/api/auth/me", "", first, nil), 200)
	checkStatus(t, h.request("GET", "/api/auth/me", "", second, nil), 401)
	checkStatus(t, h.request("POST", "/api/auth/login", `{"username":"alice","password":"password-123"}`, nil, nil), 401)
	h.login(t, "alice", "new-password")
}

func TestAdminUsersBanRolesAndPolicy(t *testing.T) {
	h := newHarness(t)
	root, adminCookie := h.administrator(t)
	alice, c := h.register(t, "alice")
	path := "/api/admin/users/" + strconv.FormatInt(alice.ID, 10)
	checkStatus(t, h.request("POST", "/api/admin/problems", `{}`, nil, nil), 401)
	checkStatus(t, h.request("POST", "/api/admin/problems", `{}`, c, nil), 403)
	checkStatus(t, h.request("POST", "/api/admin/problems", `{}`, adminCookie, nil), 200)
	checkStatus(t, h.request("GET", "/api/admin/users", "", c, nil), 403)
	checkStatus(t, h.request("PATCH", path, `{"role":"admin"}`, c, nil), 403)
	w := h.request("GET", "/api/admin/users?page=1&page_size=1", "", adminCookie, nil)
	checkStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("hash leak")
	}
	checkStatus(t, h.request("PATCH", path, `{"status":"banned"}`, adminCookie, nil), 200)
	checkStatus(t, h.request("GET", "/api/auth/me", "", c, nil), 401)
	checkStatus(t, h.request("POST", "/api/auth/login", `{"username":"alice","password":"password-123"}`, nil, nil), 401)
	checkStatus(t, h.request("PATCH", path, `{"status":"active"}`, adminCookie, nil), 200)
	checkStatus(t, h.request("GET", "/api/auth/me", "", c, nil), 401)
	c = h.login(t, "alice", "password-123")
	selfPath := "/api/admin/users/" + strconv.FormatInt(root.ID, 10)
	checkStatus(t, h.request("PATCH", selfPath, `{"status":"banned"}`, adminCookie, nil), 400)
	checkStatus(t, h.request("PATCH", selfPath, `{"role":"user"}`, adminCookie, nil), 400)
	checkStatus(t, h.request("PATCH", path, `{"role":"superuser"}`, adminCookie, nil), 400)
	checkStatus(t, h.request("PATCH", path, `{"status":"disabled"}`, adminCookie, nil), 400)
	checkStatus(t, h.request("PATCH", path, `{}`, adminCookie, nil), 400)
	checkStatus(t, h.request("PATCH", "/api/admin/users/99999", `{"role":"user"}`, adminCookie, nil), 404)
	checkStatus(t, h.request("PATCH", path, `{"role":"admin"}`, adminCookie, nil), 200)
	checkStatus(t, h.request("GET", "/api/auth/me", "", c, nil), 401)
	c = h.login(t, "alice", "password-123")
	checkStatus(t, h.request("GET", "/api/admin/users", "", c, nil), 200)
	checkStatus(t, h.request("PATCH", selfPath, `{"role":"user"}`, adminCookie, nil), 200)
	checkStatus(t, h.request("GET", "/api/auth/me", "", adminCookie, nil), 401)
	checkStatus(t, h.request("PATCH", path, `{"role":"user"}`, c, nil), 400)
	policy := Policy{}
	owner := &model.User{ID: 1, Role: "user", Status: "active"}
	other := &model.User{ID: 2, Role: "user", Status: "active"}
	sub := &model.Submission{UserID: 1}
	if !policy.Can(owner, "view_submission", sub) || policy.Can(other, "view_submission", sub) || policy.Can(nil, "manage", nil) || policy.Can(owner, "manage", nil) || policy.Can(owner, "unknown", nil) {
		t.Fatal("policy violated")
	}
	owner.Status = "banned"
	if policy.Can(owner, "authenticated", nil) {
		t.Fatal("banned user authorized")
	}
}

func TestConcurrentLastAdminProtection(t *testing.T) {
	h := newHarness(t)
	root, rootCookie := h.administrator(t)
	alice, _ := h.register(t, "alice")
	path := "/api/admin/users/" + strconv.FormatInt(alice.ID, 10)
	checkStatus(t, h.request("PATCH", path, `{"role":"admin"}`, rootCookie, nil), 200)
	aliceCookie := h.login(t, "alice", "password-123")
	var wg sync.WaitGroup
	for _, entry := range []struct {
		id     int64
		cookie *http.Cookie
	}{{root.ID, rootCookie}, {alice.ID, aliceCookie}} {
		wg.Add(1)
		go func(id int64, c *http.Cookie) {
			defer wg.Done()
			w := h.request("PATCH", "/api/admin/users/"+strconv.FormatInt(id, 10), `{"role":"user"}`, c, nil)
			if w.Code != 200 && w.Code != 400 {
				t.Errorf("status %d", w.Code)
			}
		}(entry.id, entry.cookie)
	}
	wg.Wait()
	var n int
	if err := h.db.DB.QueryRow("SELECT count(*) FROM users WHERE role='admin' AND status='active'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("remaining admins %d: %v", n, err)
	}
}

func TestCookieSecureAndTrustedProxy(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.TrustedProxy = "127.0.0.1" })
	for _, tc := range []struct {
		name, remote, xff, proto string
		tls, secure              bool
		ip                       string
	}{
		{"direct", "192.0.2.1:42", "198.51.100.1", "https", false, false, "192.0.2.1"},
		{"trusted", "127.0.0.1:42", "203.0.113.99, 192.0.2.1", "https", false, true, "192.0.2.1"},
		{"tls", "192.0.2.1:42", "", "", true, true, "192.0.2.1"},
		{"bad-forward", "127.0.0.1:42", "invalid", "https,http", false, false, "127.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://oj.test", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", tc.xff)
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if h.s.cookieSecure(r) != tc.secure || h.s.clientIP(r) != tc.ip {
				t.Fatal("proxy trust violated")
			}
			w := httptest.NewRecorder()
			h.s.setCookie(w, r, "test-token")
			if w.Result().Cookies()[0].Secure != tc.secure {
				t.Fatal("cookie secure mismatch")
			}
		})
	}
	r := httptest.NewRequest("POST", "http://oj.test", nil)
	r.RemoteAddr = "127.0.0.1:42"
	r.Header.Set("X-Forwarded-Proto", "https")
	if !h.s.sameOrigin(r, "https://oj.test") {
		t.Fatal("trusted external scheme rejected")
	}
	h.s.cfg.CookieSecure = "true"
	r.RemoteAddr = "192.0.2.1:42"
	if !h.s.cookieSecure(r) {
		t.Fatal("forced secure ignored")
	}
	h.s.cfg.CookieSecure = "false"
	r.TLS = &tls.ConnectionState{}
	if h.s.cookieSecure(r) {
		t.Fatal("forced insecure ignored")
	}
}

func TestBootstrapLogsGeneratedPasswordOnlyOnce(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	h := newHarness(t, func(c *config.Config) { c.AdminPassword = "" })
	if err := h.s.EnsureAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := logs.String()
	if !strings.Contains(first, "仅显示一次") {
		t.Fatal("bootstrap disclosure missing")
	}
	if err := h.s.EnsureAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logs.String() != first {
		t.Fatal("generated password logged more than once")
	}
	var count int
	h.db.DB.QueryRow("SELECT count(*) FROM users WHERE role='admin'").Scan(&count)
	if count != 1 {
		t.Fatal("duplicate admin")
	}
	logs.Reset()
	configured := newHarness(t)
	if err := configured.s.EnsureAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	configured.login(t, "admin", "admin-password")
	if logs.Len() != 0 {
		t.Fatal("configured password or hash logged")
	}
}
