package server

import (
	"fmt"
	"math"
	"oj/internal/model"
	"strings"
)

func validateProblem(p *model.Problem, maxCode int64) string {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" || len([]rune(p.Title)) > 120 {
		return "标题必须为 1–120 个字符"
	}
	if p.Difficulty != "easy" && p.Difficulty != "medium" && p.Difficulty != "hard" {
		return "难度必须为简单、中等或困难"
	}
	if len(p.Description) == 0 || len(p.Description) > 200000 {
		return "题目描述不能为空且不得超过 200KB"
	}
	for _, s := range []string{p.InputFormat, p.OutputFormat, p.Hint} {
		if len(s) > 100000 {
			return "输入、输出格式或提示不得超过 100KB"
		}
	}
	if p.TimeLimitMS < 1 || p.TimeLimitMS > 30000 {
		return "基准时限应为 1–30000ms"
	}
	if p.MemoryLimitMB < 16 || p.MemoryLimitMB > 2048 {
		return "基准内存应为 16–2048MB"
	}
	if p.Checker == "" {
		p.Checker = "trim"
	}
	if p.Checker != "exact" && p.Checker != "trim" && p.Checker != "float" {
		return "比较方式无效"
	}
	if p.Checker == "float" && (p.FloatEps <= 0 || p.FloatEps > 1 || math.IsNaN(p.FloatEps) || math.IsInf(p.FloatEps, 0)) {
		return "浮点误差应大于 0 且不超过 1"
	}
	if p.Checker != "float" && (p.FloatEps <= 0 || p.FloatEps > 1) {
		p.FloatEps = 1e-6
	}
	if len(p.Tags) > 12 {
		return "最多设置 12 个标签"
	}
	seen := map[string]bool{}
	tags := []string{}
	for _, tag := range p.Tags {
		tag = strings.TrimSpace(tag)
		if len([]rune(tag)) < 1 || len([]rune(tag)) > 24 {
			return "每个标签应为 1–24 个字符"
		}
		if !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	p.Tags = tags
	if len(p.Testcases) > 200 {
		return "每题最多 200 个用例（含样例）"
	}
	graded, total := 0, 0
	for i := range p.Testcases {
		t := &p.Testcases[i]
		t.Index = i + 1
		if len(t.Input) > 1<<20 || len(t.Expected) > 1<<20 {
			return fmt.Sprintf("用例 %d 的输入或输出超过 1MB", i+1)
		}
		if len(t.Explanation) > 10000 {
			return "样例解释不得超过 10KB"
		}
		total += len(t.Input) + len(t.Expected)
		if t.SampleOnly && !t.IsSample {
			return "仅展示的用例必须是公开样例"
		}
		if !t.SampleOnly {
			graded++
		}
	}
	if graded < 1 {
		return "至少需要 1 个参与判题的用例"
	}
	if total > 16<<20 {
		return "单题测试数据总量不得超过 16MB"
	}
	if len(p.Solutions) > 3 {
		return "最多保存 3 份参考答案"
	}
	solutionLanguages := map[string]bool{}
	for _, s := range p.Solutions {
		if solutionLanguages[s.Language] {
			return "同一种语言只能保存一份参考答案"
		}
		solutionLanguages[s.Language] = true
		if len(s.Language) < 1 || len(s.Language) > 32 || len(s.Code) == 0 || int64(len(s.Code)) > maxCode {
			return "参考答案语言或代码大小无效"
		}
	}
	for l, c := range p.Templates {
		if l == "" || len(l) > 32 || int64(len(c)) > maxCode {
			return "起始模板语言或代码大小无效"
		}
	}
	for l, v := range p.LangLimits {
		if l == "" || len(l) > 32 || v.TimeLimitMS < 0 || v.TimeLimitMS > 120000 || v.MemoryLimitMB < 0 || v.MemoryLimitMB > 4096 || (v.MemoryLimitMB > 0 && v.MemoryLimitMB < 16) {
			return "语言覆盖限制无效（时限最多 120000ms，内存 16–4096MB）"
		}
	}
	return ""
}
