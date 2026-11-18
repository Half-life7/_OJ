package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"oj/internal/model"
)

func pageNumber(raw string, fallback, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request) {
	if admin(w, r) == nil {
		return
	}
	page := pageNumber(r.URL.Query().Get("page"), 1, 1000000)
	size := pageNumber(r.URL.Query().Get("page_size"), 20, 100)
	q := r.URL.Query().Get("q")
	if len(q) > 100 {
		fail(w, 400, "validation_error", "搜索内容过长")
		return
	}
	where := ""
	args := []any{}
	if q != "" {
		where = ` WHERE username LIKE ? ESCAPE '\'`
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)+"%")
	}
	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM users"+where, args...).Scan(&total); err != nil {
		internal(w)
		return
	}
	args = append(args, size, (page-1)*size)
	rows, err := tx.QueryContext(r.Context(), "SELECT "+userColumns+" FROM users"+where+" ORDER BY id LIMIT ? OFFSET ?", args...)
	if err != nil {
		internal(w)
		return
	}
	items := []model.User{}
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			rows.Close()
			internal(w)
			return
		}
		u.PasswordHash = ""
		items = append(items, *u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		internal(w)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}

func (s *Service) patchUser(w http.ResponseWriter, r *http.Request) {
	actor := admin(w, r)
	if actor == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "validation_error", "用户编号无效")
		return
	}
	var in struct {
		Role   *string `json:"role"`
		Status *string `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role == nil && in.Status == nil {
		fail(w, 400, "validation_error", "请指定角色或账号状态")
		return
	}
	if in.Role != nil && *in.Role != "user" && *in.Role != "admin" {
		fail(w, 400, "validation_error", "角色必须为 user 或 admin")
		return
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "banned" {
		fail(w, 400, "validation_error", "账号状态必须为 active 或 banned")
		return
	}
	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var authorized int
	session, _ := r.Context().Value(sessionKey).(string)
	if err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM users u JOIN sessions ss ON ss.user_id=u.id WHERE u.id=? AND u.role='admin' AND u.status='active' AND ss.token_hash=? AND ss.expires_at>?`, actor.ID, session, s.clock().Unix()).Scan(&authorized); err != nil {
		internal(w)
		return
	}
	if authorized != 1 {
		fail(w, 403, "forbidden", "没有权限")
		return
	}
	u, err := scanUser(tx.QueryRowContext(r.Context(), "SELECT "+userColumns+" FROM users WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "用户不存在")
		return
	}
	if err != nil {
		internal(w)
		return
	}
	role, status := u.Role, u.Status
	if in.Role != nil {
		role = *in.Role
	}
	if in.Status != nil {
		status = *in.Status
	}
	if id == actor.ID && status == "banned" {
		fail(w, 400, "self_ban", "不能封禁自己")
		return
	}
	if u.Role == "admin" && u.Status == "active" && (role != "admin" || status != "active") {
		var count int
		if err = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM users WHERE role='admin' AND status='active'").Scan(&count); err != nil {
			internal(w)
			return
		}
		if count <= 1 {
			fail(w, 400, "last_admin", "不能封禁或降级最后一位管理员")
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE users SET role=?,status=? WHERE id=?", role, status, id); err != nil {
		internal(w)
		return
	}
	if status == "banned" || role != u.Role {
		if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id=?", id); err != nil {
			internal(w)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		internal(w)
		return
	}
	u.Role = role
	u.Status = status
	u.PasswordHash = ""
	respond(w, 200, map[string]any{"user": u})
}
