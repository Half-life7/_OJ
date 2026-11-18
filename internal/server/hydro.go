package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"oj/internal/auth"
	"oj/internal/model"
)

const (
	maxHydroFiles    = 4000
	maxHydroProblems = 100
	maxHydroExpanded = 64 << 20
)

type hydroStatement struct {
	Name   string `yaml:"name"`
	Lang   string `yaml:"lang"`
	Source string `yaml:"source"`
	File   string `yaml:"file,omitempty"`
}

type hydroMetadata struct {
	PID        string           `yaml:"pid"`
	Title      string           `yaml:"title"`
	Name       string           `yaml:"name,omitempty"`
	Tag        []string         `yaml:"tag,omitempty"`
	Difficulty int              `yaml:"difficulty,omitempty"`
	Statements []hydroStatement `yaml:"statements,omitempty"`
	Content    yaml.Node        `yaml:"content,omitempty"`
}

type hydroCase struct {
	Input  string `yaml:"input"`
	Output string `yaml:"output"`
}

type hydroConfig struct {
	Type        string             `yaml:"type,omitempty"`
	Time        string             `yaml:"time,omitempty"`
	Memory      string             `yaml:"memory,omitempty"`
	CheckerType string             `yaml:"checker_type,omitempty"`
	Cases       []hydroCase        `yaml:"cases,omitempty"`
	Detail      any                `yaml:"detail,omitempty"`
	TimeRate    map[string]float64 `yaml:"time_limit_rate,omitempty"`
	MemoryRate  map[string]float64 `yaml:"memory_limit_rate,omitempty"`
}

var hydroLanguages = map[string]string{"cpp17": "cc", "python3": "py3", "java": "java"}

type hydroFiles map[string]*zip.File

func readHydroZIP(data []byte) ([]model.Problem, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("无法读取 Hydro ZIP：%w", err)
	}
	if len(archive.File) > maxHydroFiles {
		return nil, fmt.Errorf("Hydro ZIP 文件数超过 %d", maxHydroFiles)
	}
	files := make(hydroFiles)
	var roots []string
	var expanded uint64
	for _, file := range archive.File {
		name := strings.TrimPrefix(strings.TrimSuffix(file.Name, "/"), "./")
		if file.FileInfo().IsDir() && name == "." {
			continue
		}
		if !validHydroPath(name) || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) {
			return nil, fmt.Errorf("ZIP 含不支持的路径或文件类型：%q", file.Name)
		}
		if file.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		if files[name] != nil {
			return nil, fmt.Errorf("ZIP 中文件名重复：%s", name)
		}
		if file.UncompressedSize64 > maxHydroExpanded-expanded {
			return nil, fmt.Errorf("ZIP 解压后不得超过 64 MB")
		}
		expanded += file.UncompressedSize64
		files[name] = file
		if path.Base(name) == "problem.yaml" {
			dir := path.Dir(name)
			if dir == "." {
				dir = ""
			} else {
				dir += "/"
			}
			roots = append(roots, dir)
		}
	}
	if len(roots) == 0 || len(roots) > maxHydroProblems {
		return nil, fmt.Errorf("Hydro ZIP 应包含 1–%d 个 problem.yaml", maxHydroProblems)
	}
	sort.Strings(roots)
	problems := make([]model.Problem, 0, len(roots))
	total := 0
	for _, root := range roots {
		problem, err := files.problem(root)
		if err != nil {
			return nil, fmt.Errorf("%sproblem.yaml：%w", root, err)
		}
		for _, tc := range problem.Testcases {
			total += len(tc.Input) + len(tc.Expected)
		}
		if total > maxHydroExpanded {
			return nil, fmt.Errorf("导入题目的测试数据总量超过 64 MB")
		}
		problems = append(problems, problem)
	}
	return problems, nil
}

func validHydroPath(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 1024 && utf8.ValidString(name) &&
		!strings.ContainsAny(name, "\\:\x00\n\r") && !strings.HasPrefix(name, "/") &&
		!strings.HasPrefix(name, "../") && name == path.Clean(name)
}

func (files hydroFiles) text(name string, limit int64) (string, error) {
	file := files[name]
	if file == nil {
		return "", fmt.Errorf("缺少文件 %s", name)
	}
	if file.UncompressedSize64 > uint64(limit) {
		return "", fmt.Errorf("文件 %s 超过 %d KB", name, limit>>10)
	}
	reader, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("无法打开 %s：%w", name, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(data)) > limit || !utf8.Valid(data) {
		return "", fmt.Errorf("文件 %s 损坏、过大或未使用 UTF-8 编码", name)
	}
	return string(data), nil
}

func (files hydroFiles) problem(root string) (model.Problem, error) {
	var p model.Problem
	var meta hydroMetadata
	data, err := files.text(root+"problem.yaml", 200<<10)
	if err != nil {
		return p, err
	}
	if err := yaml.Unmarshal([]byte(data), &meta); err != nil {
		return p, fmt.Errorf("题目配置无效：%w", err)
	}
	p.Title = meta.Title
	if p.Title == "" {
		p.Title = meta.Name
	}
	p.Tags = meta.Tag
	p.Difficulty = "medium"
	switch {
	case meta.Difficulty >= 1 && meta.Difficulty <= 3:
		p.Difficulty = "easy"
	case meta.Difficulty >= 8:
		p.Difficulty = "hard"
	}
	statement, err := files.statement(root, meta)
	if err != nil {
		return p, err
	}
	if strings.Contains(statement, "file://") {
		return p, fmt.Errorf("题面引用了 Hydro 附件，请先将附件链接改为公开 URL")
	}
	if err := parseStatement(statement, &p, false, nil); err != nil {
		return p, err
	}
	for i := range p.Testcases {
		p.Testcases[i].SampleOnly = true
	}
	config, err := files.config(root)
	if err != nil {
		return p, err
	}
	if config.Type != "" && config.Type != "default" {
		return p, fmt.Errorf("暂不支持 Hydro 题型 %q", config.Type)
	}
	switch config.CheckerType {
	case "", "default":
		p.Checker = "trim"
	case "strict":
		p.Checker = "exact"
	default:
		return p, fmt.Errorf("暂不支持 Hydro 比较器 %q", config.CheckerType)
	}
	if p.TimeLimitMS, err = hydroLimit(config.Time, 1000, false); err != nil {
		return p, err
	}
	if p.MemoryLimitMB, err = hydroLimit(config.Memory, 256, true); err != nil {
		return p, err
	}
	p.LangLimits = make(map[string]model.Limits)
	for local, hydro := range hydroLanguages {
		time, err := applyHydroRate(config.TimeRate[hydro], p.TimeLimitMS, 120000)
		if err != nil {
			return p, fmt.Errorf("%s 的 Hydro 时限设置无效：%w", hydro, err)
		}
		memory, err := applyHydroRate(config.MemoryRate[hydro], p.MemoryLimitMB, 4096)
		if err != nil {
			return p, fmt.Errorf("%s 的 Hydro 内存设置无效：%w", hydro, err)
		}
		if time != 0 || memory != 0 {
			p.LangLimits[local] = model.Limits{TimeLimitMS: time, MemoryLimitMB: memory}
		}
	}
	cases := config.Cases
	if len(cases) == 0 {
		cases, err = files.pairedCases(root + "testdata/")
		if err != nil {
			return p, err
		}
	}
	if len(cases) == 0 || len(cases)+len(p.Testcases) > 200 {
		return p, fmt.Errorf("每道题至少需要 1 组测试数据，最多 200 个用例（含样例）")
	}
	total := 0
	for _, tc := range p.Testcases {
		total += len(tc.Input) + len(tc.Expected)
	}
	for _, test := range cases {
		input, err := files.testdata(root, test.Input)
		if err != nil {
			return p, err
		}
		output, err := files.testdata(root, test.Output)
		if err != nil {
			return p, err
		}
		total += len(input) + len(output)
		if total > 16<<20 {
			return p, fmt.Errorf("单题测试数据总量不得超过 16 MB")
		}
		p.Testcases = append(p.Testcases, model.Testcase{Input: input, Expected: output})
	}
	return p, nil
}

func (files hydroFiles) statement(root string, meta hydroMetadata) (string, error) {
	if len(meta.Statements) != 0 {
		var first, preferred string
		for _, info := range meta.Statements {
			if info.Source == "" || info.File != "" || !validHydroPath(info.Source) || strings.Contains(info.Source, "/") {
				continue
			}
			ext := strings.ToLower(path.Ext(info.Source))
			if ext != ".md" && ext != ".markdown" {
				continue
			}
			if first == "" {
				first = info.Source
			}
			if strings.HasPrefix(strings.ToLower(info.Lang), "zh") && preferred == "" {
				preferred = info.Source
			}
		}
		if preferred == "" {
			preferred = first
		}
		if preferred == "" {
			return "", fmt.Errorf("题包没有可导入的 Markdown 题面")
		}
		return files.text(root+preferred, 512<<10)
	}
	for _, folder := range []string{"", "statement/", "problem_statement/"} {
		for _, name := range []string{"problem_zh.md", "problem_zh-CN.md", "problem.md", "problem_en.md"} {
			if name := root + folder + name; files[name] != nil {
				return files.text(name, 512<<10)
			}
		}
	}
	if meta.Content.Kind == yaml.ScalarNode && meta.Content.Tag == "!!str" {
		var localized map[string]string
		if strings.HasPrefix(strings.TrimSpace(meta.Content.Value), "{") && yaml.Unmarshal([]byte(meta.Content.Value), &localized) == nil {
			return chooseHydroLanguage(localized)
		}
		return meta.Content.Value, nil
	}
	if meta.Content.Kind == yaml.MappingNode {
		var localized map[string]string
		if err := meta.Content.Decode(&localized); err == nil {
			return chooseHydroLanguage(localized)
		}
	}
	return "", fmt.Errorf("缺少 Markdown 题面（problem.md 或 problem_zh.md）")
}

func chooseHydroLanguage(localized map[string]string) (string, error) {
	for _, language := range []string{"zh", "zh-CN", "en"} {
		if text := localized[language]; text != "" {
			return text, nil
		}
	}
	languages := make([]string, 0, len(localized))
	for language, text := range localized {
		if text != "" {
			languages = append(languages, language)
		}
	}
	sort.Strings(languages)
	if len(languages) != 0 {
		return localized[languages[0]], nil
	}
	return "", fmt.Errorf("题面为空")
}

func (files hydroFiles) config(root string) (hydroConfig, error) {
	var config hydroConfig
	for _, name := range []string{"config.yaml", "config.yml", "Config.yaml", "Config.yml"} {
		name = root + "testdata/" + name
		if files[name] == nil {
			continue
		}
		data, err := files.text(name, 200<<10)
		if err != nil {
			return config, err
		}
		decoder := yaml.NewDecoder(strings.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&config); err != nil && err != io.EOF {
			return config, fmt.Errorf("testdata/config.yaml 包含无效或不支持的配置：%w", err)
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return config, fmt.Errorf("testdata/config.yaml 只允许一个 YAML 文档")
		}
		return config, nil
	}
	return config, nil
}

var hydroNumber = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)([a-z]*)$`)

func hydroLimit(value string, fallback int64, memory bool) (int64, error) {
	if value == "" {
		return fallback, nil
	}
	match := hydroNumber.FindStringSubmatch(strings.ToLower(strings.TrimSpace(value)))
	if match != nil {
		number, err := strconv.ParseFloat(match[1], 64)
		units := map[string]float64{"": 1, "ms": 1, "s": 1000, "us": .001}
		if memory {
			units = map[string]float64{"": 1, "m": 1, "mb": 1, "mib": 1, "k": 1.0 / 1024, "kb": 1.0 / 1024, "kib": 1.0 / 1024, "g": 1024, "gb": 1024, "gib": 1024}
		}
		if unit := units[match[2]]; err == nil && unit > 0 && number*unit <= 1<<31 {
			if memory {
				return int64(math.Ceil(number * unit)), nil
			}
			return int64(number * unit), nil
		}
	}
	return 0, fmt.Errorf("无法解析 Hydro 资源限制 %q", value)
}

func applyHydroRate(rate float64, base, maximum int64) (int64, error) {
	if rate == 0 {
		return 0, nil
	}
	limit := math.Round(rate * float64(base))
	if math.IsNaN(limit) || limit < 1 || limit > float64(maximum) {
		return 0, fmt.Errorf("倍率 %v 超出支持的资源范围", rate)
	}
	return int64(limit), nil
}

var (
	hydroSequence = regexp.MustCompile(`[0-9]+`)
	backtickRun   = regexp.MustCompile("`+")
)

func (files hydroFiles) pairedCases(prefix string) ([]hydroCase, error) {
	var inputs []string
	outputs := map[string]string{}
	for filename := range files {
		name, ok := strings.CutPrefix(filename, prefix)
		if !ok || strings.Contains(name, "/") {
			continue
		}
		ext := path.Ext(name)
		switch strings.ToLower(ext) {
		case ".in":
			inputs = append(inputs, name)
		case ".out", ".ans":
			stem := strings.TrimSuffix(name, ext)
			if outputs[stem] != "" {
				return nil, fmt.Errorf("测试数据 %s 有多个输出文件，请在 config.yaml 的 cases 字段指定配对", stem)
			}
			outputs[stem] = name
		}
	}
	key := func(name string) string {
		return hydroSequence.ReplaceAllStringFunc(name, func(digits string) string {
			return fmt.Sprintf("%010s", digits)
		})
	}
	sort.Slice(inputs, func(i, j int) bool {
		a, b := key(inputs[i]), key(inputs[j])
		if a == b {
			return inputs[i] < inputs[j]
		}
		return a < b
	})
	cases := make([]hydroCase, 0, len(inputs))
	for _, input := range inputs {
		output := outputs[strings.TrimSuffix(input, path.Ext(input))]
		if output == "" {
			return nil, fmt.Errorf("测试数据 %s 没有配对的 .out 或 .ans", input)
		}
		cases = append(cases, hydroCase{Input: input, Output: output})
	}
	return cases, nil
}

func (files hydroFiles) testdata(root, name string) (string, error) {
	if name == "/dev/null" {
		return "", nil
	}
	if !validHydroPath(name) {
		return "", fmt.Errorf("无效测试数据文件名 %q", name)
	}
	return files.text(root+"testdata/"+name, 1<<20)
}

func (h *Server) importHydro(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "manage", nil) {
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 400, "invalid_archive", "无法读取 ZIP，上传文件不得超过 24 MB")
		return
	}
	problems, err := readHydroZIP(data)
	if err != nil {
		fail(w, 400, "invalid_archive", err.Error())
		return
	}
	for i := range problems {
		p := &problems[i]
		p.CreatedBy = auth.UserFromContext(r.Context()).ID
		if msg := h.validateProblemInput(p); msg != "" {
			fail(w, 400, "validation_error", fmt.Sprintf("第 %d 题 %q：%s", i+1, p.Title, msg))
			return
		}
	}
	if err := h.store.CreateProblems(r.Context(), problems); err != nil {
		internal(w)
		return
	}
	type imported struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	items := make([]imported, 0, len(problems))
	for _, p := range problems {
		items = append(items, imported{p.ID, p.Title})
	}
	write(w, 201, map[string]any{"items": items})
}

func (h *Server) exportHydro(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r, "manage", nil) {
		return
	}
	p, ok := h.getProblem(w, r, true)
	if !ok {
		return
	}
	if p.Checker == "float" {
		fail(w, 400, "unsupported_export", "浮点容差比较需要专用 Hydro 比较器，当前导出支持普通比较")
		return
	}
	for language := range p.LangLimits {
		if hydroLanguages[language] == "" {
			fail(w, 400, "unsupported_export", "分语言限制包含没有 Hydro 映射的语言："+language)
			return
		}
	}
	var buffer bytes.Buffer
	if err := writeHydroZIP(&buffer, p); err != nil {
		internal(w)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="P%d-hydro.zip"`, p.ID))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = buffer.WriteTo(w)
}

func writeHydroZIP(output io.Writer, p *model.Problem) error {
	writer := zip.NewWriter(output)
	root := fmt.Sprintf("P%d/", p.ID)
	add := func(name, text string) error {
		file, err := writer.Create(root + name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(file, text)
		return err
	}
	meta := hydroMetadata{
		PID: fmt.Sprintf("P%d", p.ID), Title: p.Title, Tag: p.Tags,
		Difficulty: map[string]int{"easy": 2, "medium": 5, "hard": 8}[p.Difficulty],
		Statements: []hydroStatement{{Name: "zh.md", Lang: "zh", Source: "problem_zh.md"}},
	}
	metadata, err := yaml.Marshal(meta)
	if err != nil {
		return err
	}
	if err := add("problem.yaml", string(metadata)); err != nil {
		return err
	}
	if err := add("problem_zh.md", hydroMarkdown(p)); err != nil {
		return err
	}
	config := hydroConfig{Type: "default", Time: fmt.Sprintf("%dms", p.TimeLimitMS), Memory: fmt.Sprintf("%dm", p.MemoryLimitMB), CheckerType: "default"}
	if p.Checker == "exact" {
		config.CheckerType = "strict"
	}
	config.TimeRate, config.MemoryRate = make(map[string]float64), make(map[string]float64)
	for language, limit := range p.LangLimits {
		hydro := hydroLanguages[language]
		if limit.TimeLimitMS != 0 {
			config.TimeRate[hydro] = float64(limit.TimeLimitMS) / float64(p.TimeLimitMS)
		}
		if limit.MemoryLimitMB != 0 {
			config.MemoryRate[hydro] = float64(limit.MemoryLimitMB) / float64(p.MemoryLimitMB)
		}
	}
	for _, tc := range p.Testcases {
		if tc.SampleOnly {
			continue
		}
		index := len(config.Cases) + 1
		test := hydroCase{Input: fmt.Sprintf("%d.in", index), Output: fmt.Sprintf("%d.out", index)}
		if err := add("testdata/"+test.Input, tc.Input); err != nil {
			return err
		}
		if err := add("testdata/"+test.Output, tc.Expected); err != nil {
			return err
		}
		config.Cases = append(config.Cases, test)
	}
	configuration, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := add("testdata/config.yaml", string(configuration)); err != nil {
		return err
	}
	return writer.Close()
}

func hydroMarkdown(p *model.Problem) string {
	var out strings.Builder
	for _, section := range []struct{ heading, text string }{
		{"题目描述", p.Description}, {"输入格式", p.InputFormat}, {"输出格式", p.OutputFormat}, {"提示", p.Hint},
	} {
		if section.text != "" {
			fmt.Fprintf(&out, "## %s\n\n%s\n\n", section.heading, section.text)
		}
	}
	index := 0
	for _, tc := range p.Testcases {
		if !tc.IsSample {
			continue
		}
		index++
		if index == 1 {
			out.WriteString("## 样例\n\n")
		}
		for _, block := range []struct{ kind, data string }{{"input", tc.Input}, {"output", tc.Expected}} {
			fenceLength := 3
			for _, run := range backtickRun.FindAllStringIndex(block.data, -1) {
				fenceLength = max(fenceLength, run[1]-run[0]+1)
			}
			fence := strings.Repeat("`", fenceLength)
			fmt.Fprintf(&out, "%s%s%d\n%s", fence, block.kind, index, block.data)
			if block.data != "" && !strings.HasSuffix(block.data, "\n") {
				out.WriteByte('\n')
			}
			fmt.Fprintf(&out, "%s\n\n", fence)
		}
		if tc.Explanation != "" {
			fmt.Fprintf(&out, "**样例 %d 解释**\n\n%s\n\n", index, tc.Explanation)
		}
	}
	return out.String()
}
