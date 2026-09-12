package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/yudhiesh-oc/outline-cli/internal/cli"
)

func fakeAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("OUTLINE_URL", server.URL)
	t.Setenv("OUTLINE_API_KEY", "fixture-token")
	t.Setenv("OUTLINE_CONFIG", "")
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func document(t *testing.T, args ...string) map[string]any {
	t.Helper()
	code, out, errOut := runCLI(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, errOut)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEditsPreserveOmittedFieldsAndEmptyReplacements(t *testing.T) {
	text, title := "Keep\nRemove\nEnd", "Original"
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		switch r.URL.Path {
		case "/api/documents.update":
			if v, ok := body["title"].(string); ok {
				title = v
			}
			if v, ok := body["text"].(string); ok {
				switch body["editMode"] {
				case "patch":
					text = strings.Replace(text, body["findText"].(string), v, 1)
				case "append":
					text += v
				default:
					text = v
				}
			}
		case "/api/documents.info":
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"id": "d1", "title": title, "text": text}})
	})
	document(t, "update", "d1", "--patch", "--find", "Remove\n", "--text", "")
	if got := document(t, "get", "d1"); got["text"] != "Keep\nEnd" || got["title"] != "Original" {
		t.Fatalf("patch deletion damaged document: %v", got)
	}
	document(t, "update", "d1", "--title", "")
	if got := document(t, "get", "d1"); got["title"] != "" || got["text"] != "Keep\nEnd" {
		t.Fatalf("title update replaced body or lost empty value: %v", got)
	}
	document(t, "update", "d1", "--text", "")
	if got := document(t, "get", "d1")["text"]; got != "" {
		t.Fatalf("clear body = %v", got)
	}
	document(t, "update", "d1", "--text", "--raw", "--append")
	if got := document(t, "get", "d1")["text"]; got != "--raw" {
		t.Fatalf("flag-looking text consumed as output option: %v", got)
	}
}

func TestDiscoverySummariesAndRawPreserveMeaning(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true,"data":[{"document":{"id":"d1","title":"Runbook","text":"large body","data":{"type":"doc"},"revision":9007199254740993},"context":"matching steps","ranking":0.9}]}`)
	})
	code, out, errOut := runCLI(t, "search", "runbook")
	if code != 0 {
		t.Fatal(errOut)
	}
	var hits []struct {
		Document map[string]json.RawMessage `json:"document"`
		Context  string                     `json:"context"`
		Ranking  float64                    `json:"ranking"`
	}
	if err := json.Unmarshal([]byte(out), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || string(hits[0].Document["id"]) != `"d1"` || hits[0].Context != "matching steps" || hits[0].Ranking != 0.9 {
		t.Fatalf("lost search meaning: %s", out)
	}
	if _, ok := hits[0].Document["text"]; ok {
		t.Fatal("discovery returned a document body")
	}
	if _, ok := hits[0].Document["data"]; ok {
		t.Fatal("discovery returned editor data")
	}
	code, out, errOut = runCLI(t, "search", "runbook", "--raw")
	if code != 0 {
		t.Fatal(errOut)
	}
	if err := json.Unmarshal([]byte(out), &hits); err != nil {
		t.Fatal(err)
	}
	if string(hits[0].Document["text"]) != `"large body"` || string(hits[0].Document["revision"]) != "9007199254740993" {
		t.Fatalf("raw lost fields or rounded integers: %s", out)
	}
}

func TestListAscendingDirection(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		items := []map[string]string{{"title": "Alpha"}, {"title": "Zulu"}}
		// Outline treats every value except exact ASC as descending.
		if body["direction"] != "ASC" {
			slices.Reverse(items)
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": items})
	})
	code, out, errOut := runCLI(t, "list", "--sort", "title", "--direction", "asc")
	var items []map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &items) != nil || len(items) != 2 || items[0]["title"] != "Alpha" {
		t.Fatalf("not ascending: %s; %s", out, errOut)
	}
}

func TestSearchPaginationKeepsQueryAndStopsAtTotal(t *testing.T) {
	calls := 0
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Query  string
			Offset int
			Limit  int
		}
		json.NewDecoder(r.Body).Decode(&body)
		rows := []map[string]any{}
		for i, title := range []string{"runbook A", "runbook B", "unrelated"} {
			if strings.Contains(title, body.Query) {
				rows = append(rows, map[string]any{"document": map[string]any{"id": i, "title": title}})
			}
		}
		total := len(rows)
		start := min(body.Offset, total)
		rows = rows[start:min(start+body.Limit, total)]
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": rows, "pagination": map[string]any{
			"total": total, "limit": body.Limit, "nextPath": "/api/documents.search?limit=1&offset=1",
		}})
	})
	code, out, errOut := runCLI(t, "search", "runbook", "--limit", "1", "--all")
	var hits []struct{ Document struct{ Title string } }
	if code != 0 || json.Unmarshal([]byte(out), &hits) != nil || len(hits) != 2 || hits[1].Document.Title != "runbook B" || calls != 2 {
		t.Fatalf("filtered pages: code=%d calls=%d out=%s err=%s", code, calls, out, errOut)
	}
}

func TestUnsafeOrInvalidInputsDoNotReachAPI(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached server") })
	cases := [][]string{
		{"update", "d1", "--append", "--patch", "--find", "old", "--text", "new"},
		{"update", "d1", "--patch", "--find", "old"},
		{"update", "d1", "--patch", "--text", "new"},
		{"update", "d1", "--find", "old", "--text", "new"},
		{"update", "d1", "--append", "--text", ""},
		{"update", "d1", "--append"},
		{"update", "d1"},
		{"update", "d1", "--publish=false"},
		{"update", "d1", "--collection", "00000000-0000-0000-0000-000000000001"},
		{"list", "--query", "absent"},
		{"list", "--direction", "sideways"},
		{"search", "x", "--limit", "0"},
		{"users", "--offset", "-1"},
		{"move", "d1", "--index", "-1"},
		{"create", "title", "--collection", "not-a-uuid"},
		{"api", "../documents.delete"},
		{"api", "documents.info", "--data", "null"},
		{"api", "documents.info", "--data", "[]"},
	}
	for _, args := range cases {
		if code, out, _ := runCLI(t, args...); code != 2 || out != "" {
			t.Errorf("%v: exit=%d out=%s", args, code, out)
		}
	}
}

func TestDelimiterPreservesLiteralTitle(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Title string }
		json.NewDecoder(r.Body).Decode(&body)
		if body.Title != "--raw" {
			http.Error(w, "wrong title", 400)
			return
		}
		io.WriteString(w, `{"ok":true,"data":{"id":"created","title":"--raw"}}`)
	})
	if got := document(t, "create", "--publish=false", "--", "--raw"); got["id"] != "created" {
		t.Fatalf("literal title not created: %v", got)
	}
}

func TestHelpRemainsALiteralDocumentTitle(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Title string }
		json.NewDecoder(r.Body).Decode(&body)
		if body.Title != "help" {
			http.Error(w, "wrong title", http.StatusBadRequest)
			return
		}
		io.WriteString(w, `{"ok":true,"data":{"id":"created","title":"help"}}`)
	})
	if got := document(t, "create", "help", "--publish=false"); got["id"] != "created" {
		t.Fatalf("help title was intercepted as a command: %v", got)
	}
}

func TestAPIKeepsLargeRequestIntegersAndSuccessOnlyJSON(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if string(body["revision"]) != "9007199254740993" {
			http.Error(w, "wrong revision", 409)
			return
		}
		io.WriteString(w, `{"ok":true,"success":true}`)
	})
	if got := document(t, "api", "documents.update", "--data", `{"revision":9007199254740993}`); got["success"] != true {
		t.Fatalf("success-only response missing: %v", got)
	}
}

type failedWriter struct{ short bool }

func (w failedWriter) Write(b []byte) (int, error) {
	if w.short {
		return len(b) - 1, nil
	}
	return 0, errors.New("closed output")
}

func TestOutputFailureDoesNotRetryMutation(t *testing.T) {
	calls := 0
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"ok":true,"success":true}`)
	})
	for _, short := range []bool{false, true} {
		before := calls
		var stderr bytes.Buffer
		code := cli.Run(t.Context(), []string{"delete", "d1"}, failedWriter{short}, &stderr)
		if code != 1 || calls != before+1 || stderr.Len() == 0 {
			t.Fatalf("output failure: exit=%d calls=%d stderr=%s", code, calls, &stderr)
		}
	}
}

func TestHelpAndMissingCredentialsNeverCallAPI(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	t.Setenv("OUTLINE_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	for _, command := range []string{"", "search", "get", "list", "create", "update", "move", "archive", "restore", "delete", "collections", "tree", "comments", "comment", "users", "templates", "api"} {
		args := []string{"--help"}
		if command != "" {
			args = append([]string{command}, args...)
		}
		if code, _, _ := runCLI(t, args...); code != 0 {
			t.Errorf("%v: exit=%d", args, code)
		}
	}
	if code, out, _ := runCLI(t, "get", "d1"); code != 1 || out != "" {
		t.Fatalf("missing credentials: code=%d out=%s", code, out)
	}
}

func TestVersionDoesNotCallAPI(t *testing.T) {
	calls := 0
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Errorf("unexpected request")
	})
	code, out, errOut := runCLI(t, "--version")
	if code != 0 || !strings.Contains(out, "dev") || errOut != "" || calls != 0 {
		t.Fatalf("version: exit=%d out=%q err=%q calls=%d", code, out, errOut, calls)
	}
}

func TestMoveReceiptPreservesAffectedDocuments(t *testing.T) {
	fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true,"data":{"documents":[{"id":"parent","collectionId":"target","title":"Moved","text":"large parent body"},{"id":"child","collectionId":"target","parentDocumentId":"parent","text":"large child body"}],"collections":[]}}`)
	})
	got := document(t, "move", "parent", "--collection", "00000000-0000-0000-0000-000000000001")
	items, ok := got["documents"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("move lost affected documents: %v", got)
	}
	child := items[1].(map[string]any)
	if child["id"] != "child" || child["collectionId"] != "target" || child["parentDocumentId"] != "parent" {
		t.Fatalf("move lost hierarchy: %v", child)
	}
	if _, ok := child["text"]; ok {
		t.Fatal("move receipt returned the full child body")
	}
}
