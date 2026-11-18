// Package seed embeds the initial problems and reference sources. It never
// compiles or runs submitted code or reference solutions.
package seed

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"oj/internal/model"
	"oj/internal/store"
)

//go:embed problems/*.json solutions/*/*
var assets embed.FS

// Load returns independent, fully populated problems from embedded assets.
// No working-directory files or executable processes are needed.
func Load() ([]model.Problem, error) {
	files, err := fs.Glob(assets, "problems/*.json")
	if err != nil {
		return nil, err
	}
	if len(files) != 5 {
		return nil, fmt.Errorf("seed: expected five problem assets, got %d", len(files))
	}
	problems := make([]model.Problem, 0, len(files))
	for _, name := range files {
		data, err := assets.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var p model.Problem
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("seed %s: %w", name, err)
		}
		slug := strings.TrimSuffix(path.Base(name), ".json")
		for _, source := range []struct{ language, filename string }{
			{"cpp17", "main.cpp"}, {"python3", "main.py"}, {"java", "Main.java"},
		} {
			code, err := assets.ReadFile("solutions/" + slug + "/" + source.filename)
			if err != nil {
				return nil, fmt.Errorf("seed %s %s: %w", slug, source.language, err)
			}
			if len(strings.TrimSpace(string(code))) == 0 {
				return nil, fmt.Errorf("seed %s: empty %s solution", slug, source.language)
			}
			p.Solutions = append(p.Solutions, model.Solution{Language: source.language, Code: string(code)})
		}
		samples := 0
		for i := range p.Testcases {
			p.Testcases[i].Index = i + 1
			if p.Testcases[i].IsSample {
				samples++
			}
		}
		if p.Title == "" || len(p.Testcases) < 6 || samples < 2 {
			return nil, fmt.Errorf("seed %s: title, six tests and two samples required", slug)
		}
		problems = append(problems, p)
	}
	return problems, nil
}

// Import atomically initializes an empty problem bank after EnsureAdmin.
// Existing problems, including private ones, are preserved. The store repeats
// the emptiness check inside its transaction to handle concurrent startup.
func Import(ctx context.Context, s *store.Store) error {
	if s == nil || s.DB == nil {
		return errors.New("seed: nil store")
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM problems").Scan(&count); err != nil {
		return fmt.Errorf("seed: count problems: %w", err)
	}
	if count > 0 {
		return nil
	}
	problems, err := Load()
	if err != nil {
		return err
	}
	var adminID int64
	if err := s.DB.QueryRowContext(ctx, "SELECT id FROM users WHERE role='admin' ORDER BY id LIMIT 1").Scan(&adminID); err != nil {
		return fmt.Errorf("seed: administrator required; call EnsureAdmin first: %w", err)
	}
	for i := range problems {
		problems[i].CreatedBy = adminID
	}
	if err := s.ImportProblemsIfEmpty(ctx, problems); err != nil {
		return fmt.Errorf("seed: import transaction: %w", err)
	}
	return nil
}
