// Package sandbox executes untrusted programs exclusively through an isolated backend.
package sandbox

import (
	"context"
	"errors"

	"oj/internal/model"
)

type Language struct {
	ID            string   `json:"id" yaml:"id"`
	Display       string   `json:"display" yaml:"display"`
	Source        string   `json:"source" yaml:"source"`
	Compile       []string `json:"compile" yaml:"compile"`
	Run           []string `json:"run" yaml:"run"`
	Probe         []string `json:"probe" yaml:"probe"`
	TimeFactor    float64  `json:"time_factor" yaml:"time_factor"`
	MemoryFactor  float64  `json:"memory_factor" yaml:"memory_factor"`
	TimeExtraMS   int64    `json:"time_extra_ms" yaml:"time_extra_ms"`
	MemoryExtraMB int64    `json:"memory_extra_mb" yaml:"memory_extra_mb"`
	ProcLimit     int      `json:"proc_limit" yaml:"proc_limit"`
	Highlight     string   `json:"highlight" yaml:"highlight"`
	Template      string   `json:"template" yaml:"template"`
	Artifact      string   `json:"artifact" yaml:"artifact"`
}

type CompileRequest struct {
	Language Language
	Code     string
	Limits   model.Limits
	CPUSet   string
}

type CompileResult struct {
	ArtifactID string `json:"artifact_id"`
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	TimeUs     int64  `json:"time_us"`
	TimeMS     int64  `json:"time_ms"`
	MemoryKB   int64  `json:"memory_kb"`
}

type RunRequest struct {
	Language   Language
	ArtifactID string
	Input      string
	Limits     model.Limits
	CPUSet     string
}

type RunResult struct {
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	TimeUs     int64  `json:"time_us"`
	TimeMS     int64  `json:"time_ms"`
	WallTimeUs int64  `json:"wall_time_us"`
	MemoryKB   int64  `json:"memory_kb"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated"`
}

type Sandbox interface {
	Probe(context.Context, Language) error
	Compile(context.Context, CompileRequest) (CompileResult, error)
	Run(context.Context, RunRequest) (RunResult, error)
	Release(context.Context, string) error
}

var ErrUnavailable = errors.New("sandbox unavailable")
var ErrInvalidRequest = errors.New("invalid sandbox request")

// isolate deliberately fails closed until a real isolated adapter is configured.
type isolate struct{}

func NewIsolate() Sandbox                             { return isolate{} }
func (isolate) Probe(context.Context, Language) error { return ErrUnavailable }
func (isolate) Compile(context.Context, CompileRequest) (CompileResult, error) {
	return CompileResult{Status: "system_error"}, ErrUnavailable
}
func (isolate) Run(context.Context, RunRequest) (RunResult, error) {
	return RunResult{Status: "system_error"}, ErrUnavailable
}
func (isolate) Release(context.Context, string) error { return nil }
