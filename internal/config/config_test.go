package config

import (
	"os"
	"strings"
	"testing"
)

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"PORT", "DB_PATH", "SANDBOX", "SANDBOX_URL", "SANDBOX_TOKEN", "LANGUAGES_FILE", "COOKIE_SECURE", "TRUSTED_PROXY", "ADMIN_USERNAME", "ADMIN_PASSWORD", "JUDGE_WORKERS", "JUDGE_CPUSET", "RERUN_BORDERLINE", "ALLOW_REGISTER", "MAX_CODE_BYTES", "ARGON_MEMORY_KB", "ARGON_ITERATIONS", "ARGON_PARALLELISM", "DEV"} {
		old, ok := os.LookupEnv(k)
		_ = os.Unsetenv(k)
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(k, old)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}
func TestConfigurationDefaults(t *testing.T) {
	cleanEnv(t)
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if c.Port != "8080" || c.MaxCodeBytes != 65536 || !c.AllowRegister || !c.RerunBorderline || c.Workers < 1 || c.ArgonMemory < 19456 || c.ArgonIterations < 2 {
		t.Fatalf("invalid defaults: %+v", c)
	}
}
func TestCPUSetWorkerLimits(t *testing.T) {
	cleanEnv(t)
	t.Setenv("JUDGE_CPUSET", "2,3,4")
	c, e := Load()
	if e != nil || c.Workers != 3 {
		t.Fatalf("workers=%d error=%v", c.Workers, e)
	}
	t.Setenv("JUDGE_WORKERS", "4")
	if _, e = Load(); e == nil {
		t.Fatal("accepted overlapping workers")
	}
	t.Setenv("JUDGE_WORKERS", "2")
	t.Setenv("JUDGE_CPUSET", "2,2")
	if _, e = Load(); e == nil {
		t.Fatal("accepted duplicate CPU")
	}
}
func TestBadConfigurationRejected(t *testing.T) {
	cases := map[string]string{"PORT": "0", "SANDBOX": "shell", "SANDBOX_URL": "file:///tmp/sandbox", "COOKIE_SECURE": "yes", "TRUSTED_PROXY": "all", "MAX_CODE_BYTES": "-1", "ARGON_MEMORY_KB": "1", "ARGON_ITERATIONS": "1", "ALLOW_REGISTER": "yes", "ADMIN_USERNAME": "a;sh", "ADMIN_PASSWORD": "short"}
	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv(k, v)
			if _, e := Load(); e == nil {
				t.Fatalf("accepted invalid %s", k)
			}
		})
	}
}
func TestUsernameValidation(t *testing.T) {
	for _, s := range []string{"abc", "User_123", strings.Repeat("x", 20)} {
		if !ValidUsername(s) {
			t.Errorf("rejected %q", s)
		}
	}
	for _, s := range []string{"ab", strings.Repeat("x", 21), "中文名", "user-name", "abc\n"} {
		if ValidUsername(s) {
			t.Errorf("accepted %q", s)
		}
	}
}
