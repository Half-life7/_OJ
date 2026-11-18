package auth

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// The dev preflight is intentionally before CSRF: browsers do not include
		// the requested header in the OPTIONS request itself.
		if s.cfg.Dev && origin != "" {
			if !s.allowedOrigin(r, origin) {
				fail(w, 403, "csrf_failed", "请求来源不受信任")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Requested-With") != "oj" || (origin != "" && !s.allowedOrigin(r, origin)) || len(r.Header.Values("Origin")) > 1 {
				fail(w, 403, "csrf_failed", "请求安全校验失败，请刷新后重试")
				return
			}
		}
		cookie, err := r.Cookie(sessionCookie)
		if err == nil {
			if len(cookie.Value) != 43 {
				s.setCookie(w, r, "")
			} else {
				hash := tokenHash(cookie.Value)
				u, err := scanUser(s.store.DB.QueryRowContext(r.Context(), `SELECT u.id,u.username,u.email,u.password_hash,u.role,u.status,u.created_at,u.last_login_at FROM users u JOIN sessions ss ON ss.user_id=u.id WHERE ss.token_hash=? AND ss.expires_at>? AND u.status='active'`, hash, s.clock().Unix()))
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					internal(w)
					return
				}
				if err == nil {
					// Hashes stay within credential handlers, even for in-process
					// callers that accidentally format a context user.
					u.PasswordHash = ""
					ctx := context.WithValue(r.Context(), userKey, u)
					ctx = context.WithValue(ctx, sessionKey, hash)
					r = r.WithContext(ctx)
				} else {
					s.setCookie(w, r, "")
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String()
	}
	return "unknown"
}
func (s *Service) trusted(r *http.Request) bool {
	proxy, err := netip.ParseAddr(s.cfg.TrustedProxy)
	if err != nil {
		return false
	}
	peer, err := netip.ParseAddr(remoteIP(r))
	return err == nil && peer.Unmap() == proxy.Unmap()
}
func (s *Service) clientIP(r *http.Request) string {
	peer := remoteIP(r)
	if !s.trusted(r) {
		return peer
	}
	// Walk from the proxy side. A client-supplied leftmost XFF value must not
	// bypass rate limits when the proxy appends the actual client address.
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return peer
		}
		if ip.Unmap().String() != peer {
			return ip.Unmap().String()
		}
	}
	return peer
}
func (s *Service) requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if s.trusted(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "https"
	}
	return "http"
}
func (s *Service) cookieSecure(r *http.Request) bool {
	switch s.cfg.CookieSecure {
	case "true":
		return true
	case "false":
		return false
	}
	return s.requestScheme(r) == "https"
}
func (s *Service) sameOrigin(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Path != "" || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host) && u.Scheme == s.requestScheme(r)
}

// Development deliberately permits a frontend served by a local static server
// on another loopback port. Production only accepts the request's own origin.
func (s *Service) allowedOrigin(r *http.Request, origin string) bool {
	if s.sameOrigin(r, origin) {
		return true
	}
	if !s.cfg.Dev {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Path != "" || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
