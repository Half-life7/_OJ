package seed

import "testing"

func TestLoadEmbeddedProblemsAndSolutions(t *testing.T) {
	problems, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 5 {
		t.Fatalf("got %d problems, want 5", len(problems))
	}
	titles := map[string]bool{}
	for _, problem := range problems {
		if titles[problem.Title] {
			t.Fatalf("duplicate seed title %q", problem.Title)
		}
		titles[problem.Title] = true
		if len(problem.Testcases) < 6 {
			t.Fatalf("%s has only %d testcases", problem.Title, len(problem.Testcases))
		}
		samples := 0
		for index, testcase := range problem.Testcases {
			if testcase.Index != index+1 {
				t.Fatalf("%s testcase index = %d", problem.Title, testcase.Index)
			}
			if testcase.IsSample {
				samples++
			}
		}
		if samples < 2 {
			t.Fatalf("%s has only %d samples", problem.Title, samples)
		}
		if len(problem.Solutions) != 3 {
			t.Fatalf("%s has %d solutions", problem.Title, len(problem.Solutions))
		}
		languages := map[string]bool{}
		for _, solution := range problem.Solutions {
			if solution.Language == "" || solution.Code == "" {
				t.Fatalf("%s contains an empty solution", problem.Title)
			}
			languages[solution.Language] = true
		}
		for _, language := range []string{"cpp17", "python3", "java"} {
			if !languages[language] {
				t.Fatalf("%s is missing %s solution", problem.Title, language)
			}
		}
	}
}
