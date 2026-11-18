package server

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"oj/internal/model"
)

const apiMaxCodeBytes int64 = 65536

func apiProblem() model.Problem {
	return model.Problem{
		Title: "A+B", Difficulty: "easy", Description: "Read two integers and print their sum.",
		InputFormat: "Two integers.", OutputFormat: "Their sum.", Checker: "trim",
		TimeLimitMS: 1000, MemoryLimitMB: 128, IsPublic: true, Tags: []string{"math", "入门"},
		Testcases: []model.Testcase{
			{Input: "20 22\n", Expected: "42\n", Explanation: "20 + 22 = 42", IsSample: true},
			{Input: "1000000 -999958\n", Expected: "42\n", Explanation: "private-case-explanation-one"},
			{Input: "123456 -123414\n", Expected: "42\n", Explanation: "private-case-explanation-two"},
		},
		Solutions: []model.Solution{{Language: "python3", Code: "print(42) # reference-answer-only"}},
		Templates: map[string]string{"python3": "# Read the input\n"},
	}
}

type invalidProblemCase struct {
	name   string
	mutate func(*model.Problem)
}

// These cases are exercised both directly and through the administrator API.
func invalidProblemCases() []invalidProblemCase {
	return []invalidProblemCase{
		{"empty title", func(p *model.Problem) { p.Title = " \t\n " }},
		{"long Unicode title", func(p *model.Problem) { p.Title = strings.Repeat("题", 121) }},
		{"unknown difficulty", func(p *model.Problem) { p.Difficulty = "impossible" }},
		{"missing description", func(p *model.Problem) { p.Description = "" }},
		{"large description", func(p *model.Problem) { p.Description = strings.Repeat("a", 200001) }},
		{"large input format", func(p *model.Problem) { p.InputFormat = strings.Repeat("a", 100001) }},
		{"large output format", func(p *model.Problem) { p.OutputFormat = strings.Repeat("a", 100001) }},
		{"large hint", func(p *model.Problem) { p.Hint = strings.Repeat("a", 100001) }},
		{"zero time", func(p *model.Problem) { p.TimeLimitMS = 0 }},
		{"negative time", func(p *model.Problem) { p.TimeLimitMS = -1 }},
		{"excessive time", func(p *model.Problem) { p.TimeLimitMS = 30001 }},
		{"small memory", func(p *model.Problem) { p.MemoryLimitMB = 15 }},
		{"excessive memory", func(p *model.Problem) { p.MemoryLimitMB = 2049 }},
		{"unknown checker", func(p *model.Problem) { p.Checker = "shell" }},
		{"zero float epsilon", func(p *model.Problem) { p.Checker, p.FloatEps = "float", 0 }},
		{"negative float epsilon", func(p *model.Problem) { p.Checker, p.FloatEps = "float", -0.1 }},
		{"excessive float epsilon", func(p *model.Problem) { p.Checker, p.FloatEps = "float", 1.01 }},
		{"too many tags", func(p *model.Problem) {
			p.Tags = nil
			for i := 0; i < 13; i++ {
				p.Tags = append(p.Tags, fmt.Sprintf("tag%d", i))
			}
		}},
		{"blank tag", func(p *model.Problem) { p.Tags = []string{" "} }},
		{"long Unicode tag", func(p *model.Problem) { p.Tags = []string{strings.Repeat("标", 25)} }},
		{"no cases", func(p *model.Problem) { p.Testcases = nil }},
		{"too many cases", func(p *model.Problem) { p.Testcases = manyAPICases(201, "", "") }},
		{"no graded case", func(p *model.Problem) {
			for i := range p.Testcases {
				p.Testcases[i].IsSample = true
				p.Testcases[i].SampleOnly = true
			}
		}},
		{"large case input", func(p *model.Problem) { p.Testcases[0].Input = strings.Repeat("a", (1<<20)+1) }},
		{"large case output", func(p *model.Problem) { p.Testcases[0].Expected = strings.Repeat("a", (1<<20)+1) }},
		{"large explanation", func(p *model.Problem) { p.Testcases[0].Explanation = strings.Repeat("a", 10001) }},
		{"excessive total case data", func(p *model.Problem) {
			p.Testcases = manyAPICases(8, strings.Repeat("a", 1<<20), strings.Repeat("b", 1<<20))
			p.Testcases = append(p.Testcases, model.Testcase{Input: "a"})
		}},
		{"too many solutions", func(p *model.Problem) {
			p.Solutions = []model.Solution{{Language: "python3", Code: "x"}, {Language: "python3", Code: "x"}, {Language: "python3", Code: "x"}, {Language: "python3", Code: "x"}}
		}},
		{"duplicate solution language", func(p *model.Problem) {
			p.Solutions = append(p.Solutions, model.Solution{Language: "python3", Code: "print(42)"})
		}},
		{"empty solution language", func(p *model.Problem) { p.Solutions[0].Language = "" }},
		{"long solution language", func(p *model.Problem) { p.Solutions[0].Language = strings.Repeat("x", 33) }},
		{"empty solution", func(p *model.Problem) { p.Solutions[0].Code = "" }},
		{"large solution in bytes", func(p *model.Problem) { p.Solutions[0].Code = strings.Repeat("字", int(apiMaxCodeBytes)/3+1) }},
		{"empty template language", func(p *model.Problem) { p.Templates = map[string]string{"": "x"} }},
		{"long template language", func(p *model.Problem) { p.Templates = map[string]string{strings.Repeat("x", 33): "x"} }},
		{"large template", func(p *model.Problem) {
			p.Templates = map[string]string{"python3": strings.Repeat("x", int(apiMaxCodeBytes)+1)}
		}},
		{"empty override language", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{"": {}} }},
		{"long override language", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{strings.Repeat("x", 33): {}} }},
		{"negative override time", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{"python3": {TimeLimitMS: -1}} }},
		{"excessive override time", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{"python3": {TimeLimitMS: 120001}} }},
		{"negative override memory", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{"python3": {MemoryLimitMB: -1}} }},
		{"small override memory", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{"python3": {MemoryLimitMB: 15}} }},
		{"excessive override memory", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{"python3": {MemoryLimitMB: 4097}} }},
	}
}

func manyAPICases(n int, input, expected string) []model.Testcase {
	cases := make([]model.Testcase, n)
	for i := range cases {
		cases[i] = model.Testcase{Input: input, Expected: expected, IsSample: i == 0}
	}
	return cases
}

func TestValidateProblemRejectsInvalidData(t *testing.T) {
	for _, tc := range invalidProblemCases() {
		t.Run(tc.name, func(t *testing.T) {
			p := apiProblem()
			tc.mutate(&p)
			if msg := validateProblem(&p, apiMaxCodeBytes); msg == "" {
				t.Fatal("invalid problem was accepted")
			}
		})
	}
	for name, eps := range map[string]float64{"NaN": math.NaN(), "positive infinity": math.Inf(1), "negative infinity": math.Inf(-1)} {
		t.Run(name, func(t *testing.T) {
			p := apiProblem()
			p.Checker, p.FloatEps = "float", eps
			if msg := validateProblem(&p, apiMaxCodeBytes); msg == "" {
				t.Fatal("non-finite epsilon was accepted")
			}
		})
	}
}

func TestValidateProblemAcceptsBoundariesAndNormalizes(t *testing.T) {
	tests := []invalidProblemCase{
		{"minimum limits", func(p *model.Problem) { p.TimeLimitMS, p.MemoryLimitMB = 1, 16 }},
		{"maximum limits", func(p *model.Problem) { p.TimeLimitMS, p.MemoryLimitMB = 30000, 2048 }},
		{"maximum text lengths", func(p *model.Problem) {
			p.Title = strings.Repeat("题", 120)
			p.Description = strings.Repeat("a", 200000)
			p.InputFormat, p.OutputFormat, p.Hint = strings.Repeat("a", 100000), strings.Repeat("b", 100000), strings.Repeat("c", 100000)
			p.Testcases[0].Explanation = strings.Repeat("a", 10000)
		}},
		{"maximum tags", func(p *model.Problem) {
			p.Tags = nil
			for i := 0; i < 12; i++ {
				p.Tags = append(p.Tags, strings.Repeat("标", 22)+fmt.Sprintf("%02d", i))
			}
		}},
		{"maximum case count", func(p *model.Problem) { p.Testcases = manyAPICases(200, "", "") }},
		{"maximum case bytes and total", func(p *model.Problem) {
			p.Testcases = manyAPICases(8, strings.Repeat("a", 1<<20), strings.Repeat("b", 1<<20))
		}},
		{"exact checker", func(p *model.Problem) { p.Checker = "exact" }},
		{"float checker", func(p *model.Problem) { p.Checker, p.FloatEps = "float", 1e-6 }},
		{"maximum epsilon", func(p *model.Problem) { p.Checker, p.FloatEps = "float", 1 }},
		{"inherited overrides", func(p *model.Problem) { p.LangLimits = map[string]model.Limits{"python3": {}} }},
		{"maximum overrides", func(p *model.Problem) {
			p.LangLimits = map[string]model.Limits{"python3": {TimeLimitMS: 120000, MemoryLimitMB: 4096}}
		}},
		{"maximum code bytes", func(p *model.Problem) {
			p.Solutions[0].Code = strings.Repeat("x", int(apiMaxCodeBytes))
			p.Templates = map[string]string{"python3": p.Solutions[0].Code}
		}},
		{"three distinct solutions", func(p *model.Problem) {
			p.Solutions = []model.Solution{{Language: "python3", Code: "print(42)"}, {Language: "cpp17", Code: "int main() {}"}, {Language: "java", Code: "class Main {}"}}
		}},
		{"optional solutions and templates", func(p *model.Problem) { p.Solutions, p.Templates, p.LangLimits = nil, nil, nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := apiProblem()
			tc.mutate(&p)
			if msg := validateProblem(&p, apiMaxCodeBytes); msg != "" {
				t.Fatalf("valid boundary rejected: %s", msg)
			}
		})
	}

	p := apiProblem()
	p.Title, p.Checker, p.Tags = " \tA+B\n", "", []string{" math ", "math", "入门", " 入门 "}
	for i := range p.Testcases {
		p.Testcases[i].Index = 99
	}
	if msg := validateProblem(&p, apiMaxCodeBytes); msg != "" {
		t.Fatal(msg)
	}
	if p.Title != "A+B" || p.Checker != "trim" || strings.Join(p.Tags, ",") != "math,入门" {
		t.Fatalf("normalization failed: title=%q checker=%q tags=%v", p.Title, p.Checker, p.Tags)
	}
	for i, tc := range p.Testcases {
		if tc.Index != i+1 {
			t.Errorf("case %d has index %d", i, tc.Index)
		}
	}
}
