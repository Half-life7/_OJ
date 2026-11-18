package server

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"oj/internal/model"
)

func hydroFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestHydroImportMarkdownAndExport(t *testing.T) {
	h := newAPIHarness(t)
	admin, user := h.admin(t), h.user(t, "hydro_editor")
	packageData := hydroFixture(t, map[string]string{
		"pack/first/problem.yaml":         "pid: P1000\ntitle: A+B\ndifficulty: 2\ntag: [math]\nstatements:\n- name: zh.md\n  lang: zh\n  source: statement.zh.md\n",
		"pack/first/statement.zh.md":      "# Description\n\nAdd two integers.\n\n## Input\nTwo integers.\n\n## Output\nTheir sum.\n\n# Samples\n```input1\n1 2\n```\n```output1\n3\n```\n```input2\n3 4\n```\n```output2\n7\n```\n",
		"pack/first/testdata/config.yaml": "time: 1.5s\nmemory: 256mb\nchecker_type: strict\ntime_limit_rate: {py3: 2}\ncases:\n- input: large.in\n  output: answer.ans\n",
		"pack/first/testdata/large.in":    "PRIVATE_DATA\r\n" + strings.Repeat("x", 256<<10),
		"pack/first/testdata/answer.ans":  "original answer\n",
		"pack/second/problem.yaml":        "title: Second\n",
		"pack/second/problem.md":          "No sample in this statement.\n",
		"pack/second/testdata/2.in":       "2\n",
		"pack/second/testdata/2.out":      "2\n",
		"pack/second/testdata/10.in":      "10\n",
		"pack/second/testdata/10.ans":     "10\n",
	})
	endpoint := "/api/admin/problems/hydro"
	res := h.raw(t, user, "POST", endpoint, packageData, map[string]string{"Content-Type": "application/zip"})
	requireAPIStatus(t, res, 403)
	res = h.raw(t, admin, "POST", endpoint, packageData, map[string]string{"Content-Type": "application/zip"})
	requireAPIStatus(t, res, 201)
	items := decodeAPI[struct {
		Items []model.Problem `json:"items"`
	}](t, res).Items
	if len(items) != 2 {
		t.Fatalf("expected two problems, got %s", res.body)
	}
	root := fmt.Sprintf("/api/admin/problems/%d", items[0].ID)
	res = h.request(t, admin, "GET", root+"?compact=1", nil)
	requireAPIStatus(t, res, 200)
	compact := decodeAPI[struct {
		model.Problem
		Hidden []testSummary `json:"hidden_testcases"`
	}](t, res)
	if compact.IsPublic || compact.TimeLimitMS != 1500 || compact.Checker != "exact" || compact.InputFormat != "Two integers." || compact.LangLimits["python3"].TimeLimitMS != 3000 ||
		len(compact.Testcases) != 2 || !compact.Testcases[0].SampleOnly || !compact.Testcases[1].SampleOnly ||
		len(compact.Hidden) != 1 || compact.Hidden[0].InputBytes < 256<<10 || bytes.Contains(res.body, []byte("PRIVATE_DATA")) {
		t.Fatalf("unexpected compact problem: %s", clippedAPI(res.body))
	}
	reference := compact.Hidden[0].Index
	caseURL := fmt.Sprintf("%s/testcases/%d?version=%s", root, reference, url.QueryEscape(compact.UpdatedAt))
	requireAPIStatus(t, h.request(t, user, "GET", caseURL, nil), 403)
	loaded := h.request(t, admin, "GET", caseURL, nil)
	requireAPIStatus(t, loaded, 200)
	original := decodeAPI[model.Testcase](t, loaded)
	if !strings.HasPrefix(original.Input, "PRIVATE_DATA\r\n") {
		t.Fatal("case endpoint changed the original input")
	}
	doc := fmt.Sprintf("---\ntitle: Markdown edited\nchecker: exact\nis_public: true\nlang_limits: {python3: {time_limit_ms: 3000}}\n---\n"+
		"## 题目描述\n\nEdited statement.\n\n## 测试用例\n"+
		"```input1 display-only noeol\n1 2\n```\n```output1 noeol\n3\n```\n"+
		"```input2 display-only\n3 4\n```\n```output2\n7\n```\n"+
		"```sample-note2\nSecond sample note\n```\n```test-ref%d\n```\n", reference)
	update := map[string]string{"markdown": doc, "version": compact.UpdatedAt}
	updated := h.request(t, admin, "PUT", root+"?compact=1", update)
	requireAPIStatus(t, updated, 200)
	requireAPIStatus(t, h.request(t, admin, "PUT", root, update), 409)
	p, err := h.store.GetProblem(context.Background(), items[0].ID)
	if err != nil || len(p.Testcases) != 3 || p.Testcases[2] != original || p.Testcases[0].Input != "1 2" || p.Testcases[1].Explanation != "Second sample note" {
		t.Fatalf("markdown update failed to preserve the tests: %v", err)
	}
	public := h.request(t, user, "GET", fmt.Sprintf("/api/problems/%d", p.ID), nil)
	requireAPIStatus(t, public, 200)
	if bytes.Contains(public.body, []byte("PRIVATE_DATA")) || len(decodeAPI[model.Problem](t, public).Testcases) != 2 {
		t.Fatal("public detail exposed data beyond the two samples")
	}
	requireAPIStatus(t, h.request(t, user, "GET", root+"/hydro", nil), 403)
	archive, err := admin.Get(h.server.URL + root + "/hydro")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Body.Close()
	if archive.StatusCode != 200 || archive.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("unexpected archive response: %s, %v", archive.Status, archive.Header)
	}
	archiveBody, err := io.ReadAll(archive.Body)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := readHydroZIP(archiveBody)
	if err != nil || len(exported) != 1 || len(exported[0].Testcases) != 3 || exported[0].Testcases[2].Input != original.Input || exported[0].Checker != "exact" || exported[0].LangLimits["python3"].TimeLimitMS != 3000 {
		t.Fatalf("exported package did not round trip the judged data: %v", err)
	}
	second, err := h.store.GetProblem(context.Background(), items[1].ID)
	if err != nil || second.Testcases[0].Input != "2\n" || second.Testcases[0].IsSample {
		t.Fatalf("implicit filename pairing or sample handling is incorrect: %v", err)
	}
}

func TestHydroRejectsUnsupportedBatchWithoutSaving(t *testing.T) {
	h := newAPIHarness(t)
	admin := h.admin(t)
	failing := hydroFixture(t, map[string]string{
		"a/problem.yaml":         "title: Valid\n",
		"a/problem.md":           "Statement\n",
		"a/testdata/1.in":        "1\n",
		"a/testdata/1.out":       "1\n",
		"z/problem.yaml":         "title: Unsupported\n",
		"z/problem.md":           "Statement\n",
		"z/testdata/config.yaml": "checker_type: testlib\nchecker: checker.cc\n",
	})
	res := h.raw(t, admin, "POST", "/api/admin/problems/hydro", failing, map[string]string{"Content-Type": "application/zip"})
	requireAPIStatus(t, res, 400)
	if !strings.Contains(string(res.body), "checker") {
		t.Fatalf("missing error context: %s", res.body)
	}
	list := h.request(t, admin, "GET", "/api/admin/problems", nil)
	if decodeAPI[apiProblemList](t, list).Total != 0 {
		t.Fatal("an invalid batch persisted some problems")
	}
}
