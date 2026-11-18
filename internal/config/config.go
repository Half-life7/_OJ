// Package config loads and validates process configuration.
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Config struct {
	Port, DBPath, Sandbox, SandboxURL, SandboxToken, LanguagesFile string
	CookieSecure, TrustedProxy, AdminUsername, AdminPassword       string
	Workers                                                        int
	CPUSet                                                         []string
	RerunBorderline, AllowRegister, Dev                            bool
	MaxCodeBytes                                                   int64
	ArgonMemory, ArgonIterations                                   uint32
	ArgonParallelism                                               uint8
}

// Argon2 memory is KiB. The minimum follows OWASP's Argon2id recommendation:
// https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
const DefaultArgonMemory uint32 = 19 * 1024
const DefaultArgonIterations uint32 = 2
const DefaultArgonParallelism uint8 = 1

func Load() (Config, error) {
	c := Config{Port: env("PORT", "8080"), DBPath: env("DB_PATH", "./data/oj.db"),
		Sandbox: env("SANDBOX", "gojudge"), SandboxURL: env("SANDBOX_URL", "http://127.0.0.1:5050"),
		SandboxToken: os.Getenv("SANDBOX_TOKEN"), LanguagesFile: env("LANGUAGES_FILE", "./languages.yaml"),
		CookieSecure: env("COOKIE_SECURE", "auto"), TrustedProxy: os.Getenv("TRUSTED_PROXY"),
		AdminUsername: env("ADMIN_USERNAME", "admin"), AdminPassword: os.Getenv("ADMIN_PASSWORD")}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("PORT must be between 1 and 65535")
	}
	if c.Sandbox != "gojudge" && c.Sandbox != "isolate" {
		return c, fmt.Errorf("SANDBOX must be gojudge or isolate")
	}
	u, err := url.Parse(c.SandboxURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("SANDBOX_URL must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if strings.ContainsAny(c.SandboxToken, "\r\n") {
		return c, fmt.Errorf("SANDBOX_TOKEN contains invalid characters")
	}
	if strings.TrimSpace(c.DBPath) == "" || strings.ContainsRune(c.DBPath, 0) {
		return c, fmt.Errorf("DB_PATH must be a valid nonempty path")
	}
	if strings.TrimSpace(c.LanguagesFile) == "" || strings.ContainsRune(c.LanguagesFile, 0) {
		return c, fmt.Errorf("LANGUAGES_FILE must be a valid nonempty path")
	}
	if c.CookieSecure != "auto" && c.CookieSecure != "true" && c.CookieSecure != "false" {
		return c, fmt.Errorf("COOKIE_SECURE must be auto, true, or false")
	}
	if c.TrustedProxy != "" {
		if _, err := netip.ParseAddr(c.TrustedProxy); err != nil {
			return c, fmt.Errorf("TRUSTED_PROXY must be an IP address")
		}
	}
	if !ValidUsername(c.AdminUsername) {
		return c, fmt.Errorf("ADMIN_USERNAME must contain 3-20 ASCII letters, digits, or underscores")
	}
	if c.AdminPassword != "" && !ValidPassword(c.AdminPassword) {
		return c, fmt.Errorf("ADMIN_PASSWORD must contain 8-128 characters")
	}
	if cpus := os.Getenv("JUDGE_CPUSET"); cpus != "" {
		seen := map[int]bool{}
		for _, part := range strings.Split(cpus, ",") {
			cpu, e := strconv.Atoi(strings.TrimSpace(part))
			if e != nil || cpu < 0 || cpu > 1048575 || seen[cpu] {
				return c, fmt.Errorf("JUDGE_CPUSET must contain distinct nonnegative CPU numbers")
			}
			seen[cpu] = true
			c.CPUSet = append(c.CPUSet, strconv.Itoa(cpu))
		}
	}
	workers := runtime.NumCPU() / 2
	if workers < 1 {
		workers = 1
	}
	if len(c.CPUSet) > 0 {
		workers = len(c.CPUSet)
	}
	w, err := integer("JUDGE_WORKERS", int64(workers), 1, 1024)
	if err != nil {
		return c, err
	}
	c.Workers = int(w)
	if len(c.CPUSet) > 0 && c.Workers > len(c.CPUSet) {
		return c, fmt.Errorf("JUDGE_WORKERS cannot exceed JUDGE_CPUSET core count")
	}
	if c.RerunBorderline, err = boolean("RERUN_BORDERLINE", true); err != nil {
		return c, err
	}
	if c.AllowRegister, err = boolean("ALLOW_REGISTER", true); err != nil {
		return c, err
	}
	if c.Dev, err = boolean("DEV", false); err != nil {
		return c, err
	}
	if c.MaxCodeBytes, err = integer("MAX_CODE_BYTES", 65536, 1, 16*1024*1024); err != nil {
		return c, err
	}
	m, err := integer("ARGON_MEMORY_KB", int64(DefaultArgonMemory), int64(DefaultArgonMemory), 256*1024)
	if err != nil {
		return c, err
	}
	c.ArgonMemory = uint32(m)
	i, err := integer("ARGON_ITERATIONS", int64(DefaultArgonIterations), 2, 10)
	if err != nil {
		return c, err
	}
	c.ArgonIterations = uint32(i)
	p, err := integer("ARGON_PARALLELISM", int64(DefaultArgonParallelism), 1, 8)
	if err != nil {
		return c, err
	}
	c.ArgonParallelism = uint8(p)
	return c, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
func integer(key string, fallback, minimum, maximum int64) (int64, error) {
	value := env(key, strconv.FormatInt(fallback, 10))
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < minimum || n > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minimum, maximum)
	}
	return n, nil
}
func boolean(key string, fallback bool) (bool, error) {
	v := env(key, strconv.FormatBool(fallback))
	if v != "true" && v != "false" {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return v == "true", nil
}

func ValidUsername(s string) bool {
	if len(s) < 3 || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func ValidPassword(s string) bool {
	n := utf8.RuneCountInString(s)
	return utf8.ValidString(s) && n >= 8 && n <= 128
}
