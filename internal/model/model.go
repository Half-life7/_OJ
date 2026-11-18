// Package model contains the shared persistence and API types.
package model

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email,omitempty"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	LastLoginAt  string `json:"last_login_at"`
}

type Problem struct {
	ID              int64             `json:"id"`
	Title           string            `json:"title"`
	Difficulty      string            `json:"difficulty"`
	Description     string            `json:"description"`
	InputFormat     string            `json:"input_format"`
	OutputFormat    string            `json:"output_format"`
	Hint            string            `json:"hint"`
	Checker         string            `json:"checker"`
	FloatEps        float64           `json:"float_eps"`
	TimeLimitMS     int64             `json:"time_limit_ms"`
	MemoryLimitMB   int64             `json:"memory_limit_mb"`
	IsPublic        bool              `json:"is_public"`
	CreatedBy       int64             `json:"created_by"`
	SubmitCount     int64             `json:"submit_count"`
	AcceptedCount   int64             `json:"accepted_count"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
	Status          string            `json:"status"`
	Tags            []string          `json:"tags"`
	Testcases       []Testcase        `json:"testcases"`
	TestcaseCount   int               `json:"testcase_count"`
	Templates       map[string]string `json:"templates"`
	LangLimits      map[string]Limits `json:"lang_limits"`
	Solutions       []Solution        `json:"solutions,omitempty"`
	EffectiveLimits map[string]Limits `json:"effective_limits,omitempty"`
}

type Testcase struct {
	Index       int    `json:"index"`
	Input       string `json:"input"`
	Expected    string `json:"expected"`
	Explanation string `json:"explanation"`
	IsSample    bool   `json:"is_sample"`
	SampleOnly  bool   `json:"sample_only,omitempty"`
}

type Limits struct {
	TimeLimitMS   int64 `json:"time_limit_ms"`
	MemoryLimitMB int64 `json:"memory_limit_mb"`
}

type Solution struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

type CaseResult struct {
	Index    int    `json:"index"`
	Status   string `json:"status"`
	TimeUs   int64  `json:"time_us"`
	TimeMS   int64  `json:"time_ms"`
	MemoryKB int64  `json:"memory_kb"`
	IsSample bool   `json:"is_sample"`
	Input    string `json:"input,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

type Submission struct {
	ID           int64        `json:"id"`
	ProblemID    int64        `json:"problem_id"`
	UserID       int64        `json:"user_id"`
	ProblemTitle string       `json:"problem_title"`
	Language     string       `json:"language"`
	Code         string       `json:"code"`
	Status       string       `json:"status"`
	Message      string       `json:"message"`
	CreatedAt    string       `json:"created_at"`
	JudgedAt     string       `json:"judged_at"`
	TimeUs       int64        `json:"time_us"`
	TimeMS       int64        `json:"time_ms"`
	MemoryKB     int64        `json:"memory_kb"`
	Passed       int          `json:"passed"`
	Total        int          `json:"total"`
	CaseResults  []CaseResult `json:"case_results"`
}
