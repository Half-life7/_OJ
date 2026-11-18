package server

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"oj/internal/model"
)

type problemFrontMatter struct {
	Title      string            `yaml:"title"`
	Difficulty string            `yaml:"difficulty"`
	Tags       []string          `yaml:"tags"`
	Time       int64             `yaml:"time_limit_ms"`
	Memory     int64             `yaml:"memory_limit_mb"`
	Checker    string            `yaml:"checker"`
	FloatEps   float64           `yaml:"float_eps"`
	IsPublic   bool              `yaml:"is_public"`
	Templates  map[string]string `yaml:"templates"`
	LangLimits map[string]struct {
		Time   int64 `yaml:"time_limit_ms"`
		Memory int64 `yaml:"memory_limit_mb"`
	} `yaml:"lang_limits"`
	Solutions []struct {
		Language string `yaml:"language"`
		Code     string `yaml:"code"`
	} `yaml:"solutions"`
}

func parseProblemMarkdown(text string, stored map[int]model.Testcase) (model.Problem, error) {
	text = normalizeMarkdown(text)
	front, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return model.Problem{}, fmt.Errorf("Markdown 开头需要 --- 包裹的 YAML 题目配置")
	}
	header, body, ok := strings.Cut(front, "\n---\n")
	if !ok {
		return model.Problem{}, fmt.Errorf("YAML 题目配置缺少结束分隔线 ---")
	}
	meta := problemFrontMatter{Difficulty: "medium", Time: 1000, Memory: 256, Checker: "trim"}
	decoder := yaml.NewDecoder(strings.NewReader(header))
	decoder.KnownFields(true)
	if err := decoder.Decode(&meta); err != nil {
		return model.Problem{}, fmt.Errorf("YAML 题目配置无效：%w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return model.Problem{}, fmt.Errorf("题目配置仅支持一个 YAML 文档")
	}
	p := model.Problem{
		Title: meta.Title, Difficulty: meta.Difficulty, Tags: meta.Tags,
		TimeLimitMS: meta.Time, MemoryLimitMB: meta.Memory, Checker: meta.Checker,
		FloatEps: meta.FloatEps, IsPublic: meta.IsPublic, Templates: meta.Templates,
		LangLimits: make(map[string]model.Limits, len(meta.LangLimits)),
	}
	for language, limit := range meta.LangLimits {
		p.LangLimits[language] = model.Limits{TimeLimitMS: limit.Time, MemoryLimitMB: limit.Memory}
	}
	for _, solution := range meta.Solutions {
		p.Solutions = append(p.Solutions, model.Solution{Language: solution.Language, Code: solution.Code})
	}
	err := parseStatement(body, &p, true, stored)
	return p, err
}

var (
	markdownFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")
	caseFence     = regexp.MustCompile(`^(input|output|sample-note|test-input|test-output|test-ref)([1-9][0-9]{0,8})$`)
)

type caseBlock struct {
	test                   model.Testcase
	input, output, comment bool
}

func normalizeMarkdown(text string) string {
	return strings.ReplaceAll(strings.TrimPrefix(text, "\ufeff"), "\r\n", "\n")
}

func parseStatement(text string, p *model.Problem, advanced bool, stored map[int]model.Testcase) error {
	lines := strings.SplitAfter(normalizeMarkdown(text), "\n")
	sections := map[string]*strings.Builder{
		"description": {}, "input": {}, "output": {}, "hint": {}, "samples": {}, "cases": {},
	}
	current := sections["description"]
	cases := make(map[string]*caseBlock)
	order := []*caseBlock{}
	retained := make(map[int]bool)
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\n")
		fence := markdownFence.FindStringSubmatch(line)
		if len(fence) == 0 {
			if name := statementSection(line, advanced); name != "" {
				current = sections[name]
			} else {
				current.WriteString(lines[i])
			}
			continue
		}
		end := i + 1
		for end < len(lines) && !closesFence(lines[end], fence[1]) {
			end++
		}
		fields := strings.Fields(fence[2])
		var match []string
		if len(fields) > 0 {
			match = caseFence.FindStringSubmatch(fields[0])
		}
		if len(match) == 0 || (!advanced && (strings.HasPrefix(match[1], "test-") || match[1] == "sample-note")) {
			current.WriteString(strings.Join(lines[i:min(end+1, len(lines))], ""))
			i = end
			continue
		}
		if end == len(lines) {
			return fmt.Errorf("用例代码块 %s 缺少结束围栏", fields[0])
		}
		data := strings.Join(lines[i+1:end], "")
		i = end
		kind, number := match[1], match[2]
		if kind == "test-ref" {
			index, _ := strconv.Atoi(number)
			test, ok := stored[index]
			if !ok || test.IsSample || retained[index] || len(fields) != 1 || strings.TrimSpace(data) != "" {
				return fmt.Errorf("导入用例引用 %s 无效或重复", number)
			}
			retained[index] = true
			order = append(order, &caseBlock{test: test, input: true, output: true})
			continue
		}
		sample := !strings.HasPrefix(kind, "test-")
		key := "test" + number
		if sample {
			key = "sample" + number
		}
		pair := cases[key]
		if pair == nil {
			pair = &caseBlock{test: model.Testcase{IsSample: sample}}
			cases[key] = pair
			order = append(order, pair)
		}
		for _, option := range fields[1:] {
			switch {
			case advanced && option == "noeol":
				data = strings.TrimSuffix(data, "\n")
			case advanced && option == "display-only" && sample:
				pair.test.SampleOnly = true
			case advanced:
				return fmt.Errorf("用例 %s 使用了未知选项 %s", fields[0], option)
			}
		}
		switch kind {
		case "input", "test-input":
			if pair.input {
				return fmt.Errorf("用例代码块 %s 重复", fields[0])
			}
			pair.input, pair.test.Input = true, data
		case "output", "test-output":
			if pair.output {
				return fmt.Errorf("用例代码块 %s 重复", fields[0])
			}
			pair.output, pair.test.Expected = true, data
		case "sample-note":
			if pair.comment {
				return fmt.Errorf("样例解释 %s 重复", fields[0])
			}
			pair.comment, pair.test.Explanation = true, strings.TrimSpace(data)
		}
	}
	for key, pair := range cases {
		if !pair.input || !pair.output {
			return fmt.Errorf("用例 %s 缺少配对的输入或输出代码块", key)
		}
	}
	if strings.TrimSpace(sections["cases"].String()) != "" {
		return fmt.Errorf("测试用例章节只接受输入、输出和导入用例引用代码块")
	}
	p.Description = strings.TrimSpace(sections["description"].String())
	p.InputFormat = strings.TrimSpace(sections["input"].String())
	p.OutputFormat = strings.TrimSpace(sections["output"].String())
	p.Hint = strings.TrimSpace(sections["hint"].String())
	if notes := strings.TrimSpace(sections["samples"].String()); notes != "" {
		p.Hint = strings.TrimSpace(p.Hint + "\n\n" + notes)
	}
	for _, pair := range order {
		p.Testcases = append(p.Testcases, pair.test)
	}
	return nil
}

func closesFence(line, opening string) bool {
	line, ok := trimMarkdownIndent(line)
	return ok && len(line) >= len(opening) && strings.Trim(line, opening[:1]) == ""
}

func statementSection(line string, advanced bool) string {
	line, ok := trimMarkdownIndent(line)
	if !ok {
		return ""
	}
	line, ok = strings.CutPrefix(line, "#")
	if !ok {
		return ""
	}
	line = strings.TrimPrefix(line, "#")
	if !strings.HasPrefix(line, " ") {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "题目描述", "题目说明", "描述", "description":
		return "description"
	case "输入格式", "输入", "input format", "input":
		return "input"
	case "输出格式", "输出", "output format", "output":
		return "output"
	case "提示", "hint", "hints":
		return "hint"
	case "样例", "样例输入输出", "samples", "sample":
		return "samples"
	case "测试用例":
		if advanced {
			return "cases"
		}
	}
	return ""
}

func trimMarkdownIndent(line string) (string, bool) {
	left := strings.TrimLeft(line, " ")
	return strings.TrimRight(left, " \t\n"), len(line)-len(left) <= 3
}
