package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPasswordHashNeverSerialized(t *testing.T) {
	b, err := json.Marshal(User{ID: 1, Username: "alice", PasswordHash: "sensitive-hash"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hash") || strings.Contains(string(b), "sensitive") {
		t.Fatal("password hash leaked")
	}
}
func TestOptionalCaseDataOmitted(t *testing.T) {
	b, err := json.Marshal(CaseResult{Index: 1, Status: "accepted"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"input", "expected", "actual"} {
		if strings.Contains(string(b), key) {
			t.Fatal("empty case data serialized")
		}
	}
}
