package judge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEmbeddedDefaultMatchesRepositoryOverride(t *testing.T) {
	defaults, err := LoadLanguagesOrDefault(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join("..", "..", "languages.yaml")
	if _, err = os.Stat(externalPath); err != nil {
		t.Fatal(err)
	}
	external, err := LoadLanguages(externalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaults, external) {
		t.Fatal("root languages.yaml and embedded fallback have drifted")
	}
}
