package sandbox

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"oj/internal/model"
)

type integrationProgram struct {
	source string
	want   string
	tle    string
	sleep  string
	re     string
	mle    string
	ole    string
}

var integrationPrograms = map[string]integrationProgram{
	"cpp17": {
		source: `#include <bits/stdc++.h>
using namespace std; int main(){cout<<"42\n";}`,
		want: `#include <bits/stdc++.h>
using namespace std; int main(){cout<<"41\n";}`,
		tle: `int main(){volatile unsigned long long x=1;for(;;)x=x*3+1;}`,
		sleep: `#include <unistd.h>
int main(){sleep(5);}`,
		re: `int main(){int *p=nullptr;*p=1;}`,
		mle: `#include <bits/stdc++.h>
using namespace std;int main(){vector<char>x(256u<<20);cout<<x[0];}`,
		ole: `#include <bits/stdc++.h>
using namespace std;int main(){cout<<string(2u<<20,'x');}`,
	},
	"python3": {
		source: `print(42)`,
		want:   `print(41)`,
		tle: `x=1
while True:
    x=x*3+1`,
		sleep: `import time
time.sleep(5)`,
		re: `raise RuntimeError("runtime")`,
		mle: `x=bytearray(256*1024*1024)
print(x[0])`,
		ole: `import sys
sys.stdout.write("x"*(2*1024*1024))`,
	},
	"java": {
		source: `public class Main{public static void main(String[]a){System.out.println(42);}}`,
		want:   `public class Main{public static void main(String[]a){System.out.println(41);}}`,
		tle:    `public class Main{public static void main(String[]a){long x=1;for(;;)x=x*3+1;}}`,
		sleep:  `public class Main{public static void main(String[]a)throws Exception{Thread.sleep(5000);}}`,
		re:     `public class Main{public static void main(String[]a){throw new RuntimeException();}}`,
		mle:    `public class Main{public static void main(String[]a){byte[] x=new byte[512*1024*1024];System.out.println(x[0]);}}`,
		ole:    `public class Main{public static void main(String[]a){System.out.println("x".repeat(2*1024*1024));}}`,
	},
}

func TestSandboxIntegrationVerdicts(t *testing.T) {
	if os.Getenv("OJ_SANDBOX_INTEGRATION") != "1" {
		t.Skip("set OJ_SANDBOX_INTEGRATION=1 with a real go-judge service")
	}
	languages, err := loadIntegrationLanguages()
	if err != nil {
		t.Fatal(err)
	}
	client := NewGoJudge(integrationEnv("SANDBOX_URL", "http://127.0.0.1:5050"), os.Getenv("SANDBOX_TOKEN"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for id, program := range integrationPrograms {
		language, ok := languages[id]
		if !ok {
			t.Fatalf("missing %s language", id)
		}
		if err := client.Probe(ctx, language); err != nil {
			t.Logf("%s unavailable: %v", id, err)
			continue
		}
		t.Run(id, func(t *testing.T) {
			normal := model.Limits{TimeLimitMS: 2000, MemoryLimitMB: 256}
			t.Run("AC", func(t *testing.T) {
				result, err := executeProgram(ctx, client, language, program.source, normal)
				if err != nil || result.Status != "accepted" || strings.TrimSpace(result.Stdout) != "42" {
					t.Fatalf("status=%+v err=%v", result, err)
				}
			})
			t.Run("WA-candidate", func(t *testing.T) {
				result, err := executeProgram(ctx, client, language, program.want, normal)
				if err != nil || result.Status != "accepted" || strings.TrimSpace(result.Stdout) == "42" {
					t.Fatalf("status=%+v err=%v", result, err)
				}
			})
			if id != "python3" {
				t.Run("CE", func(t *testing.T) {
					compiled, err := client.Compile(ctx, CompileRequest{Language: language, Code: "this is not valid source", Limits: normal})
					if err != nil || compiled.Status != "compile_error" {
						t.Fatalf("status=%+v err=%v", compiled, err)
					}
				})
			} else {
				t.Run("syntax-error-is-runtime", func(t *testing.T) {
					compiled, err := client.Compile(ctx, CompileRequest{Language: language, Code: "if:", Limits: normal})
					if err != nil {
						t.Fatal(err)
					}
					defer client.Release(context.Background(), compiled.ArtifactID)
					result, err := client.Run(ctx, RunRequest{Language: language, ArtifactID: compiled.ArtifactID, Limits: normal})
					if err != nil || result.Status != "runtime_error" {
						t.Fatalf("status=%+v err=%v", result, err)
					}
				})
			}
			t.Run("TLE-loop", func(t *testing.T) {
				result, err := executeProgram(ctx, client, language, program.tle, model.Limits{TimeLimitMS: 150, MemoryLimitMB: 256})
				if err != nil || result.Status != "time_limit_exceeded" {
					t.Fatalf("status=%+v err=%v", result, err)
				}
			})
			t.Run("TLE-sleep", func(t *testing.T) {
				result, err := executeProgram(ctx, client, language, program.sleep, model.Limits{TimeLimitMS: 150, MemoryLimitMB: 256})
				if err != nil || result.Status != "time_limit_exceeded" {
					t.Fatalf("status=%+v err=%v", result, err)
				}
			})
			t.Run("RE", func(t *testing.T) {
				result, err := executeProgram(ctx, client, language, program.re, normal)
				if err != nil || result.Status != "runtime_error" {
					t.Fatalf("status=%+v err=%v", result, err)
				}
			})
			t.Run("MLE", func(t *testing.T) {
				result, err := executeProgram(ctx, client, language, program.mle, model.Limits{TimeLimitMS: 3000, MemoryLimitMB: 128})
				if err != nil || result.Status != "memory_limit_exceeded" {
					// Some JVMs surface heap exhaustion as a nonzero exit before the
					// cgroup sampler observes the hard limit; report that explicitly.
					t.Fatalf("status=%+v err=%v", result, err)
				}
			})
			t.Run("OLE", func(t *testing.T) {
				result, err := executeProgram(ctx, client, language, program.ole, model.Limits{TimeLimitMS: 3000, MemoryLimitMB: 256})
				if err != nil || result.Status != "output_limit_exceeded" {
					t.Fatalf("status=%+v err=%v", result, err)
				}
			})
		})
	}
}

func executeProgram(ctx context.Context, client *GoJudge, language Language, source string, limits model.Limits) (RunResult, error) {
	compiled, err := client.Compile(ctx, CompileRequest{Language: language, Code: source, Limits: limits})
	if err != nil {
		return RunResult{}, err
	}
	defer client.Release(context.Background(), compiled.ArtifactID)
	if compiled.Status != "accepted" {
		return RunResult{Status: compiled.Status, Stderr: compiled.Stderr}, nil
	}
	return client.Run(ctx, RunRequest{Language: language, ArtifactID: compiled.ArtifactID, Limits: limits})
}

func loadIntegrationLanguages() (map[string]Language, error) {
	file, err := os.Open(filepath.Join("..", "..", "..", "languages.yaml"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var document struct {
		Languages []Language `yaml:"languages"`
	}
	if err = yaml.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&document); err != nil {
		return nil, err
	}
	result := map[string]Language{}
	for _, language := range document.Languages {
		result[language.ID] = language
	}
	return result, nil
}

func integrationEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
