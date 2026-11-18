package auth

import (
	"database/sql"
	"errors"
	"net/http"

	"oj/internal/config"
	"oj/internal/model"
)

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowRegister {
		fail(w, 403, "registration_closed", "注册已关闭")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !config.ValidUsername(in.Username) {
		fail(w, 400, "validation_error", "用户名须为 3–20 位字母、数字或下划线")
		return
	}
	if !config.ValidPassword(in.Password) {
		fail(w, 400, "validation_error", "密码须为 8–128 个字符")
		return
	}
	hash, err := s.hash(r.Context(), in.Password, s.params)
	if err != nil {
		internal(w)
		return
	}
	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM users WHERE username=?", in.Username).Scan(&exists); err != nil {
		internal(w)
		return
	}
	if exists != 0 {
		fail(w, 409, "username_taken", "用户名已被使用")
		return
	}
	created := timestamp(s.clock())
	res, err := tx.ExecContext(r.Context(), "INSERT INTO users(username,password_hash,role,status,created_at,last_login_at) VALUES(?,?,'user','active',?,?)", in.Username, hash, created, created)
	if err != nil {
		internal(w)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		internal(w)
		return
	}
	token, err := s.newSession(r.Context(), tx, r, id)
	if err != nil {
		internal(w)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w)
		return
	}
	s.setCookie(w, r, token)
	respond(w, 201, map[string]any{"user": model.User{ID: id, Username: in.Username, Role: "user", Status: "active", CreatedAt: created, LastLoginAt: created}})
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !config.ValidUsername(in.Username) {
		fail(w, 401, "invalid_credentials", "用户名或密码错误")
		return
	}
	key := s.clientIP(r) + "\x00" + normalUsername(in.Username)
	if !s.beginAttempt(key) {
		throttled(w)
		return
	}
	finished := false
	defer func() {
		if !finished {
			s.finishAttempt(key, false)
		}
	}()
	bad := func() {
		limited := s.finishAttempt(key, true)
		finished = true
		if limited {
			throttled(w)
		} else {
			fail(w, 401, "invalid_credentials", "用户名或密码错误")
		}
	}
	if !config.ValidPassword(in.Password) {
		bad()
		return
	}
	u, err := scanUser(s.store.DB.QueryRowContext(r.Context(), "SELECT "+userColumns+" FROM users WHERE username=?", in.Username))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internal(w)
		return
	}
	found := err == nil
	encoded := s.dummyHash
	if found {
		encoded = u.PasswordHash
	}
	ok, old, err := s.verify(r.Context(), in.Password, encoded)
	if err != nil {
		internal(w)
		return
	}
	if !found || !ok || u.Status != "active" {
		bad()
		return
	}
	newHash := u.PasswordHash
	if target := s.upgradedParams(old); target != old {
		newHash, err = s.hash(r.Context(), in.Password, target)
		if err != nil {
			internal(w)
			return
		}
	}
	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	fresh, err := scanUser(tx.QueryRowContext(r.Context(), "SELECT "+userColumns+" FROM users WHERE id=?", u.ID))
	if err != nil {
		internal(w)
		return
	}
	// A concurrent password reset or ban cannot be bypassed with a verification
	// that happened before the account change acquired its transaction.
	if fresh.Status != "active" || fresh.PasswordHash != u.PasswordHash {
		bad()
		return
	}
	fresh.LastLoginAt = timestamp(s.clock())
	if _, err = tx.ExecContext(r.Context(), "UPDATE users SET password_hash=?,last_login_at=? WHERE id=?", newHash, fresh.LastLoginAt, u.ID); err != nil {
		internal(w)
		return
	}
	token, err := s.newSession(r.Context(), tx, r, u.ID)
	if err != nil {
		internal(w)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w)
		return
	}
	s.finishAttempt(key, false)
	finished = true
	fresh.PasswordHash = ""
	s.setCookie(w, r, token)
	respond(w, 200, map[string]any{"user": fresh})
}
func throttled(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "600")
	fail(w, 429, "rate_limited", "登录尝试过多，请 10 分钟后重试")
}

func (s *Service) password(w http.ResponseWriter, r *http.Request) {
	u := authenticated(w, r)
	if u == nil {
		return
	}
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !config.ValidPassword(in.NewPassword) {
		fail(w, 400, "validation_error", "新密码须为 8–128 个字符")
		return
	}
	if !config.ValidPassword(in.OldPassword) {
		fail(w, 400, "invalid_password", "原密码错误")
		return
	}
	var oldHash string
	if err := s.store.DB.QueryRowContext(r.Context(), "SELECT password_hash FROM users WHERE id=?", u.ID).Scan(&oldHash); err != nil {
		internal(w)
		return
	}
	ok, old, err := s.verify(r.Context(), in.OldPassword, oldHash)
	if err != nil {
		internal(w)
		return
	}
	if !ok {
		fail(w, 400, "invalid_password", "原密码错误")
		return
	}
	hash, err := s.hash(r.Context(), in.NewPassword, s.upgradedParams(old))
	if err != nil {
		internal(w)
		return
	}
	current, _ := r.Context().Value(sessionKey).(string)
	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `UPDATE users SET password_hash=? WHERE id=? AND password_hash=? AND status='active' AND EXISTS(SELECT 1 FROM sessions WHERE token_hash=? AND user_id=? AND expires_at>?)`, hash, u.ID, oldHash, current, u.ID, s.clock().Unix())
	if err != nil {
		internal(w)
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		internal(w)
		return
	}
	if n != 1 {
		fail(w, 401, "unauthorized", "登录状态已失效，请重新登录")
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id=? AND token_hash<>?", u.ID, current); err != nil {
		internal(w)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
