package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"oj/internal/model"
)

func TestCompileRunRelease(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing bearer token")
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req struct {
			Cmd []command `json:"cmd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if len(req.Cmd) != 1 {
			t.Error("command count")
			return
		}
		c := req.Cmd[0]
		if c.CPUSet != "2" {
			t.Error("cpuset not propagated")
		}
		if c.ClockLimit <= c.CPULimit {
			t.Error("wall clock guard must allow overhead")
		}
		if len(c.CopyOutCached) > 0 {
			if !reflect.DeepEqual(c.CopyOutCached, []string{"main.jar"}) {
				t.Errorf("wrong artifact: %v", c.CopyOutCached)
			}
			if c.CopyIn["Main.java"].(map[string]any)["content"] != "source" {
				t.Error("source")
			}
			io.WriteString(w, `[{"status":"Accepted","fileIds":{"main.jar":"ARTIFACT","unused":"EXTRA"}}]`)
		} else {
			if c.CPULimit != 1500000000 || c.MemoryLimit != 268435456 {
				t.Errorf("units: %+v", c)
			}
			if c.Args[1] != "-Xmx256m" {
				t.Error("argument placeholder")
			}
			if c.CopyIn["main.jar"].(map[string]any)["fileId"] != "ARTIFACT" {
				t.Error("cache reference")
			}
			io.WriteString(w, `[{"status":"Accepted","exitStatus":0,"time":1234567,"runTime":8765432,"memory":4096,"files":{"stdout":"42\n","stderr":""}}]`)
		}
	}))
	defer server.Close()
	g := NewGoJudge(server.URL, "secret")
	l := Language{Source: "Main.java", Artifact: "main.jar", Compile: []string{"compiler"}, Run: []string{"java", "-Xmx{memory_mb}m"}, ProcLimit: 64}
	c, err := g.Compile(context.Background(), CompileRequest{Language: l, Code: "source", CPUSet: "2"})
	if err != nil || c.Status != "accepted" || c.ArtifactID != "ARTIFACT" {
		t.Fatalf("compile %+v %v", c, err)
	}
	r, err := g.Run(context.Background(), RunRequest{Language: l, ArtifactID: c.ArtifactID, Limits: model.Limits{TimeLimitMS: 1500, MemoryLimitMB: 256}, CPUSet: "2"})
	if err != nil || r.Status != "accepted" || r.Stdout != "42\n" || r.TimeUs != 1234 || r.TimeMS != 1 || r.WallTimeUs != 8765 || r.MemoryKB != 4 {
		t.Fatalf("run %+v %v", r, err)
	}
	if err = g.Release(context.Background(), c.ArtifactID); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /run", "DELETE /file/EXTRA", "POST /run", "DELETE /file/ARTIFACT"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %v", calls)
	}
}

func TestInterpretedUploadAndProbe(t *testing.T) {
	var paths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/run" {
			io.WriteString(w, `[{"status":"Accepted"}]`)
			return
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		if h.Filename != "main.py" || string(b) != "print(42)" {
			t.Error("wrong uploaded source")
		}
		io.WriteString(w, `"PYTHON"`)
	}))
	defer s.Close()
	g := NewGoJudge(s.URL, "")
	l := Language{Source: "main.py", Artifact: "main.py", Probe: []string{"python3", "--version"}}
	if err := g.Probe(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	c, err := g.Compile(context.Background(), CompileRequest{Language: l, Code: "print(42)"})
	if err != nil || c.ArtifactID != "PYTHON" {
		t.Fatalf("%+v %v", c, err)
	}
	if !reflect.DeepEqual(paths, []string{"/run", "/file"}) {
		t.Fatalf("paths %v", paths)
	}
}

func TestStatusAndBoundedOutput(t *testing.T) {
	statuses := map[string]string{"Accepted": "accepted", "Nonzero Exit Status": "runtime_error", "Signalled": "runtime_error", "Dangerous Syscall": "runtime_error", "Time Limit Exceeded": "time_limit_exceeded", "Memory Limit Exceeded": "memory_limit_exceeded", "Output Limit Exceeded": "output_limit_exceeded", "File Error": "system_error", "Internal Error": "system_error", "unexpected": "system_error"}
	for status, want := range statuses {
		t.Run(status, func(t *testing.T) {
			if got := convert(result{Status: status}).Status; got != want {
				t.Fatalf("%s != %s", got, want)
			}
		})
	}
	r := convert(result{Status: "Accepted", Files: map[string]string{"stdout": strings.Repeat("x", OutputLimitBytes+9)}})
	if !r.Truncated || len(r.Stdout) != OutputLimitBytes || r.Status != "output_limit_exceeded" {
		t.Fatal("output unbounded")
	}
	if convert(result{Status: "Accepted", ExitStatus: 1}).Status != "runtime_error" {
		t.Fatal("nonzero exit accepted")
	}
}

func TestCompileFailureCleanupAndSafeErrors(t *testing.T) {
	deletes := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			return
		}
		io.WriteString(w, `[{"status":"Nonzero Exit Status","fileIds":{"main":"FAILED"},"files":{"stderr":"syntax error"}}]`)
	}))
	defer s.Close()
	g := NewGoJudge(s.URL, "")
	c, err := g.Compile(context.Background(), CompileRequest{Language: Language{Source: "main.cpp", Artifact: "main", Compile: []string{"g++"}}})
	if err != nil || c.Status != "compile_error" || c.ArtifactID != "" || deletes != 1 || c.Stderr != "syntax error" {
		t.Fatalf("%+v %v deletes=%d", c, err, deletes)
	}
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, "secret-token /private/path")
	}))
	defer s2.Close()
	err = NewGoJudge(s2.URL, "secret-token").Probe(context.Background(), Language{Probe: []string{"probe"}})
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestCancellationAndInvalidRequests(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-finish:
		}
	}))
	defer s.Close()
	defer close(finish)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	g := NewGoJudge(s.URL, "")
	go func() { done <- g.Probe(ctx, Language{Probe: []string{"probe"}}) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation ignored")
	}
	if err := g.Release(context.Background(), "../secret"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := g.Run(context.Background(), RunRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if err := NewIsolate().Probe(context.Background(), Language{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("isolate must fail closed")
	}
}

func TestResponseContract(t *testing.T) {
	b, _ := json.Marshal(RunResult{})
	var fields map[string]any
	json.Unmarshal(b, &fields)
	want := []string{"status", "exit_code", "time_us", "time_ms", "wall_time_us", "memory_kb", "stdout", "stderr", "truncated"}
	if len(fields) != len(want) {
		t.Fatal(string(b))
	}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			t.Fatal("missing " + key)
		}
	}
}
