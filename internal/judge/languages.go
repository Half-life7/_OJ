package judge

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
	"oj/internal/judge/sandbox"
	"oj/internal/model"
)

type Language = sandbox.Language

//go:embed default_languages.yaml
var defaultLanguages []byte

func LoadLanguages(filename string) (map[string]Language, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeLanguages(f)
}

// LoadLanguagesOrDefault keeps the process runnable as a single binary. The
// external file remains the operator override and defaults to languages.yaml.
func LoadLanguagesOrDefault(filename string) (map[string]Language, error) {
	f, err := os.Open(filename)
	if err == nil {
		defer f.Close()
		return decodeLanguages(f)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return decodeLanguages(bytes.NewReader(defaultLanguages))
}

func decodeLanguages(reader io.Reader) (map[string]Language, error) {
	var doc struct {
		Languages []Language `yaml:"languages"`
	}
	d := yaml.NewDecoder(io.LimitReader(reader, 1<<20))
	d.KnownFields(true)
	if err := d.Decode(&doc); err != nil {
		return nil, fmt.Errorf("languages: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("languages: expected one YAML document")
	}
	result := make(map[string]Language)
	for _, l := range doc.Languages {
		if l.ID == "" || result[l.ID].ID != "" || strings.ContainsAny(l.ID, " /\\\t\n") {
			return nil, fmt.Errorf("languages: invalid or duplicate ID %q", l.ID)
		}
		if len(l.Run) == 0 || len(l.Probe) == 0 || l.Display == "" || l.Template == "" {
			return nil, fmt.Errorf("language %s: display, run, probe and template are required", l.ID)
		}
		for _, p := range []string{l.Source, l.Artifact} {
			if p == "" || path.Base(p) != p || p == "." || p == ".." || strings.ContainsAny(p, "\\\x00") {
				return nil, fmt.Errorf("language %s: source and artifact must be simple filenames", l.ID)
			}
		}
		if l.TimeFactor == 0 {
			l.TimeFactor = 1
		}
		if l.MemoryFactor == 0 {
			l.MemoryFactor = 1
		}
		if !validFactor(l.TimeFactor) || !validFactor(l.MemoryFactor) || l.TimeExtraMS < 0 || l.TimeExtraMS > 120000 || l.MemoryExtraMB < 0 || l.MemoryExtraMB > 4096 || l.ProcLimit < 1 || l.ProcLimit > 256 {
			return nil, fmt.Errorf("language %s: invalid resource settings", l.ID)
		}
		result[l.ID] = l
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("languages: at least one language required")
	}
	return result, nil
}

func validFactor(f float64) bool { return f > 0 && f <= 16 && !math.IsNaN(f) && !math.IsInf(f, 0) }

// EffectiveLimits applies each nonzero override independently. The caps also
// protect internal callers (bench and recovery) that do not pass API validation.
func EffectiveLimits(p *model.Problem, l Language) model.Limits {
	base := model.Limits{TimeLimitMS: 1000, MemoryLimitMB: 256}
	var override model.Limits
	if p != nil {
		if p.TimeLimitMS > 0 {
			base.TimeLimitMS = p.TimeLimitMS
		}
		if p.MemoryLimitMB > 0 {
			base.MemoryLimitMB = p.MemoryLimitMB
		}
		override = p.LangLimits[l.ID]
	}
	tf, mf := l.TimeFactor, l.MemoryFactor
	if !validFactor(tf) {
		tf = 1
	}
	if !validFactor(mf) {
		mf = 1
	}
	tm := boundedLimit(float64(base.TimeLimitMS)*tf+float64(l.TimeExtraMS), 120000)
	mm := boundedLimit(float64(base.MemoryLimitMB)*mf+float64(l.MemoryExtraMB), 4096)
	if override.TimeLimitMS > 0 {
		tm = min(override.TimeLimitMS, 120000)
	}
	if override.MemoryLimitMB > 0 {
		mm = min(override.MemoryLimitMB, 4096)
	}
	return model.Limits{TimeLimitMS: tm, MemoryLimitMB: mm}
}

func boundedLimit(n float64, cap int64) int64 {
	if math.IsNaN(n) || n < 1 {
		return 1
	}
	if n >= float64(cap) {
		return cap
	}
	return int64(math.Ceil(n))
}
