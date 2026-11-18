package auth

import (
	"strings"
	"testing"
)

func TestPHCAndOldParameterUpgrade(t *testing.T) {
	h := newHarness(t)
	u, _ := h.register(t, "alice")
	old, err := hashPassword("password-123", argonParams{memory: 64, iterations: 1, parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.db.DB.Exec("UPDATE users SET password_hash=? WHERE id=?", old, u.ID); err != nil {
		t.Fatal(err)
	}
	h.login(t, "alice", "password-123")
	var upgraded string
	if err = h.db.DB.QueryRow("SELECT password_hash FROM users WHERE id=?", u.ID).Scan(&upgraded); err != nil {
		t.Fatal(err)
	}
	ok, p := verifyPassword("password-123", upgraded)
	if !ok || p.memory < 19456 || p.iterations < 2 || upgraded == old {
		t.Fatal("legacy hash not upgraded")
	}
	if ok, _ := verifyPassword("wrong-password", upgraded); ok {
		t.Fatal("wrong password verified")
	}
	second, err := hashPassword("password-123", h.s.params)
	if err != nil {
		t.Fatal(err)
	}
	if second == upgraded {
		t.Fatal("salt reused")
	}
}

func TestMalformedPHCRejectedBeforeAllocation(t *testing.T) {
	valid, err := hashPassword("password-123", argonParams{memory: 64, iterations: 1, parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "not-a-hash", strings.Repeat("x", 600), strings.Replace(valid, "argon2id", "argon2i", 1), strings.Replace(valid, "v=19", "v=16", 1), strings.Replace(valid, "m=64", "m=4294967295", 1), strings.Replace(valid, "t=1", "t=0", 1), strings.Replace(valid, "p=1", "p=0", 1), strings.Replace(valid, "p=1", "p=255", 1), valid + "$tail", strings.Replace(valid, "m=64", "m=1", 1), "$argon2id$v=19$m=64,t=1,p=1$bad$bad"} {
		if ok, _ := verifyPassword("password-123", bad); ok {
			t.Fatal("malformed hash accepted")
		}
	}
}

func TestBoundedConcurrentLimiter(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 5; i++ {
		if !h.s.beginAttempt("same-key") {
			t.Fatal("slot rejected early")
		}
	}
	if h.s.beginAttempt("same-key") {
		t.Fatal("too many in-flight attempts")
	}
	for i := 0; i < 5; i++ {
		limited := h.s.finishAttempt("same-key", true)
		if limited != (i == 4) {
			t.Fatal("wrong fifth-failure boundary")
		}
	}
	if h.s.beginAttempt("same-key") {
		t.Fatal("failed key not limited")
	}
}
