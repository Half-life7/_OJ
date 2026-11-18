package judge

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestJavaJarStaysOnSandboxWorkMount(t *testing.T) {
	languages, err := LoadLanguagesOrDefault(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	java := languages["java"]
	for _, stage := range []struct {
		name string
		args []string
	}{{"compile", java.Compile}, {"probe", java.Probe}} {
		t.Run(stage.name, func(t *testing.T) {
			if len(stage.args) != 3 || !strings.Contains(stage.args[2], "/usr/bin/jar -J-Djava.io.tmpdir=. ") {
				t.Fatalf("jar must place temporary output on the same mount as main.jar: %q", stage.args)
			}
		})
	}
}
