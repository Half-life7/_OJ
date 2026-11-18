// Package auth is the only owner of authentication, cookies, password hashing,
// sessions, CSRF checks, and authorization policy.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"oj/internal/config"
	"oj/internal/model"
	"oj/internal/store"
)

const sessionCookie = "oj_session"
const sessionLifetime = 30 * 24 * time.Hour
const userColumns = "id,username,email,password_hash,role,status,created_at,last_login_at"

type contextKey int

const (
	userKey contextKey = iota
	sessionKey
)

type Service struct {
	store     *store.Store
	cfg       config.Config
	params    argonParams
	dummyHash string
	kdfSlots  chan struct{}
	mu        sync.Mutex
	attempts  map[string]*attempt
	clock     func() time.Time
}

func New(s *store.Store, c config.Config) (*Service, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("auth requires a store")
	}
	if c.ArgonMemory == 0 {
		c.ArgonMemory = config.DefaultArgonMemory
	}
	if c.ArgonIterations == 0 {
		c.ArgonIterations = config.DefaultArgonIterations
	}
	if c.ArgonParallelism == 0 {
		c.ArgonParallelism = config.DefaultArgonParallelism
	}
	if c.ArgonMemory < config.DefaultArgonMemory || c.ArgonMemory > 256*1024 || c.ArgonIterations < 2 || c.ArgonIterations > 10 || c.ArgonParallelism > 8 {
		return nil, errors.New("invalid Argon2 parameters")
	}
	if c.CookieSecure == "" {
		c.CookieSecure = "auto"
	}
	if c.CookieSecure != "auto" && c.CookieSecure != "true" && c.CookieSecure != "false" {
		return nil, errors.New("invalid cookie configuration")
	}
	if c.AdminUsername == "" {
		c.AdminUsername = "admin"
	}
	a := &Service{store: s, cfg: c, params: argonParams{c.ArgonMemory, c.ArgonIterations, c.ArgonParallelism}, kdfSlots: make(chan struct{}, 2), attempts: map[string]*attempt{}, clock: time.Now}
	dummy, err := randomToken()
	if err != nil {
		return nil, err
	}
	a.dummyHash, err = hashPassword(dummy, a.params)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func UserFromContext(ctx context.Context) *model.User {
	u, _ := ctx.Value(userKey).(*model.User)
	return u
}

type Policy struct{}

func (Policy) Can(user *model.User, action string, resource any) bool {
	if user == nil || user.ID <= 0 || user.Status != "active" {
		return false
	}
	switch action {
	case "authenticated":
		return true
	case "manage":
		return user.Role == "admin"
	case "view_submission":
		switch sub := resource.(type) {
		case *model.Submission:
			return sub != nil && (user.Role == "admin" || sub.UserID == user.ID)
		case model.Submission:
			return user.Role == "admin" || sub.UserID == user.ID
		}
	}
	return false
}

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/config", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]bool{"allow_register": s.cfg.AllowRegister})
	})
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("POST /api/auth/password", s.password)
	mux.HandleFunc("GET /api/admin/users", s.listUsers)
	mux.HandleFunc("PATCH /api/admin/users/{id}", s.patchUser)
}

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (*model.User, error) {
	u := new(model.User)
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt, &u.LastLoginAt)
	return u, err
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func internal(w http.ResponseWriter) {
	fail(w, 500, "internal_error", "服务暂时不可用，请稍后重试")
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		fail(w, 400, "invalid_request", "请求格式不正确或内容过长")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "invalid_request", "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}

func (s *Service) EnsureAdmin(ctx context.Context) error {
	// First check avoids unnecessary Argon2 work and password generation on every
	// restart. The transaction rechecks to arbitrate concurrent bootstrappers.
	var count int
	if err := s.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role='admin'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if !config.ValidUsername(s.cfg.AdminUsername) {
		return errors.New("invalid bootstrap admin username")
	}
	password := s.cfg.AdminPassword
	generated := password == ""
	if generated {
		var err error
		password, err = randomToken()
		if err != nil {
			return err
		}
	}
	if !config.ValidPassword(password) {
		return errors.New("invalid bootstrap admin password")
	}
	hash, err := s.hash(ctx, password, s.params)
	if err != nil {
		return err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role='admin'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,status,created_at) VALUES(?,?,'admin','active',?)", s.cfg.AdminUsername, hash, timestamp(s.clock())); err != nil {
		return errors.New("cannot create initial administrator; configured username may already exist")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// The only credential log in the application: explicitly requested first-run
	// bootstrap disclosure, after a successful commit and only when generated.
	if generated {
		log.Printf("首次管理员账号 %s 的随机密码（仅显示一次）：%s", s.cfg.AdminUsername, password)
	}
	return nil
}

func (s *Service) newSession(ctx context.Context, tx *sql.Tx, r *http.Request, userID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=?", s.clock().Unix()); err != nil {
		return "", err
	}
	// Reauthentication replaces the presented session rather than keeping a
	// fixed browser token. Other devices remain logged in.
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", tokenHash(c.Value)); err != nil {
			return "", err
		}
	}
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO sessions(token_hash,user_id,expires_at,user_agent,ip,created_at) VALUES(?,?,?,?,?,?)", tokenHash(token), userID, s.clock().Add(sessionLifetime).Unix(), ua, s.clientIP(r), timestamp(s.clock()))
	return token, err
}

func (s *Service) setCookie(w http.ResponseWriter, r *http.Request, token string) {
	c := &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cookieSecure(r), MaxAge: int(sessionLifetime / time.Second), Expires: s.clock().Add(sessionLifetime)}
	if token == "" {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}

func authenticated(w http.ResponseWriter, r *http.Request) *model.User {
	u := UserFromContext(r.Context())
	if !(Policy{}).Can(u, "authenticated", nil) {
		fail(w, 401, "unauthorized", "请先登录")
		return nil
	}
	return u
}
func admin(w http.ResponseWriter, r *http.Request) *model.User {
	u := authenticated(w, r)
	if u == nil {
		return nil
	}
	if !(Policy{}).Can(u, "manage", nil) {
		fail(w, 403, "forbidden", "没有权限")
		return nil
	}
	return u
}

func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	u := authenticated(w, r)
	if u == nil {
		return
	}
	stats, err := s.store.UserStats(r.Context(), u.ID)
	if err != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"user": u, "stats": stats})
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, err = s.store.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", tokenHash(c.Value)); err != nil {
			internal(w)
			return
		}
	}
	s.setCookie(w, r, "")
	respond(w, 200, map[string]bool{"ok": true})
}

func normalUsername(username string) string { return strings.ToLower(username) }
