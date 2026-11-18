package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type argonParams struct {
	memory, iterations uint32
	parallelism        uint8
}

func hashPassword(password string, p argonParams) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", p.memory, p.iterations, p.parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Parse before allocating: untrusted or corrupt PHC parameters cannot request
// unbounded RAM, goroutines, iterations, salt, or output sizes.
func parsePHC(encoded string) (argonParams, []byte, []byte, error) {
	var p argonParams
	bad := errors.New("invalid password hash")
	if len(encoded) > 512 {
		return p, nil, nil, bad
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return p, nil, nil, bad
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return p, nil, nil, bad
	}
	values := make([]uint64, 3)
	for i, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(params[i], prefix) {
			return p, nil, nil, bad
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(params[i], prefix), 10, 32)
		if err != nil {
			return p, nil, nil, bad
		}
		values[i] = v
	}
	if values[2] < 1 || values[2] > 8 || values[0] < 8*values[2] || values[0] > 256*1024 || values[1] < 1 || values[1] > 10 {
		return p, nil, nil, bad
	}
	p = argonParams{uint32(values[0]), uint32(values[1]), uint8(values[2])}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return p, nil, nil, bad
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return p, nil, nil, bad
	}
	return p, salt, key, nil
}

func verifyPassword(password, encoded string) (bool, argonParams) {
	p, salt, key, err := parsePHC(encoded)
	if err != nil {
		return false, p
	}
	actual := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, uint32(len(key)))
	return subtle.ConstantTimeCompare(actual, key) == 1, p
}

func (s *Service) hash(ctx context.Context, password string, p argonParams) (string, error) {
	select {
	case s.kdfSlots <- struct{}{}:
		defer func() { <-s.kdfSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return hashPassword(password, p)
}
func (s *Service) verify(ctx context.Context, password, encoded string) (bool, argonParams, error) {
	select {
	case s.kdfSlots <- struct{}{}:
		defer func() { <-s.kdfSlots }()
	case <-ctx.Done():
		return false, argonParams{}, ctx.Err()
	}
	ok, p := verifyPassword(password, encoded)
	return ok, p, nil
}

func (s *Service) upgradedParams(old argonParams) argonParams {
	p := s.params
	if old.memory > p.memory {
		p.memory = old.memory
	}
	if old.iterations > p.iterations {
		p.iterations = old.iterations
	}
	if old.parallelism > p.parallelism {
		p.parallelism = old.parallelism
	}
	return p
}
