package rewrite

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTypeString(t *testing.T) {
	cases := map[Type]string{
		Reject:             "reject",
		Reject200:          "reject-200",
		Reject204:          "reject-204",
		RejectImg:          "reject-img",
		RejectDict:         "reject-dict",
		RejectArray:        "reject-array",
		Redirect302:        "302",
		Redirect307:        "307",
		RequestHeader:      "request-header",
		RequestHeaderJSON:  "json-request-header",
		RequestBody:        "request-body",
		RequestBodyJSON:    "json-request-body",
		ResponseHeader:     "response-header",
		ResponseHeaderJSON: "json-response-header",
		ResponseBody:       "response-body",
		ResponseBodyJSON:   "json-response-body",
		Type(0):            "Unknown",
		Type(99):           "Unknown",
	}
	for typ, want := range cases {
		if got := typ.String(); got != want {
			t.Errorf("Type(%d).String() = %q, want %q", typ, got, want)
		}
	}
}

func TestParseRewrite(t *testing.T) {
	t.Run("simple reject", func(t *testing.T) {
		r, err := ParseRewrite(`^https?://example\.com url reject`)
		if err != nil {
			t.Fatalf("ParseRewrite: %v", err)
		}
		if r.Type() != Reject {
			t.Errorf("type = %v, want Reject", r.Type())
		}
	})

	t.Run("redirect with payload", func(t *testing.T) {
		r, err := ParseRewrite(`^https?://example\.com/(.*) url 302 https://example.org/$1`)
		if err != nil {
			t.Fatalf("ParseRewrite: %v", err)
		}
		if r.Type() != Redirect302 {
			t.Errorf("type = %v, want Redirect302", r.Type())
		}
		if len(r.payload) != 1 || r.payload[0] != "https://example.org/$1" {
			t.Errorf("payload = %v", r.payload)
		}
	})

	t.Run("request-header with match and replace", func(t *testing.T) {
		r, err := ParseRewrite(`^https?://example\.com url request-header (\r\n)User-Agent:.+(\r\n) request-header $1User-Agent: mihomo$2`)
		if err != nil {
			t.Fatalf("ParseRewrite: %v", err)
		}
		if r.Type() != RequestHeader {
			t.Errorf("type = %v, want RequestHeader", r.Type())
		}
		if len(r.match) != 1 || len(r.payload) != 1 {
			t.Fatalf("match/payload = %v / %v", r.match, r.payload)
		}
		if r.payload[0] != "$1User-Agent: mihomo$2" {
			t.Errorf("payload = %v", r.payload)
		}
	})

	t.Run("multi-value payload separator", func(t *testing.T) {
		r, err := ParseRewrite(`^https?://example\.com url response-body <a>old</a><and><b>old2</b> response-body <a>new</a><and><b>new2</b>`)
		if err != nil {
			t.Fatalf("ParseRewrite: %v", err)
		}
		if len(r.match) != 2 || len(r.payload) != 2 {
			t.Fatalf("match/payload = %v / %v", r.match, r.payload)
		}
		if r.payload[0] != "<a>new</a>" || r.payload[1] != "<b>new2</b>" {
			t.Errorf("payload = %v", r.payload)
		}
	})

	t.Run("json response body compiles expr", func(t *testing.T) {
		r, err := ParseRewrite(`^https?://example\.com url json-response-body data.foo == 1 && Delete("data.foo")`)
		if err != nil {
			t.Fatalf("ParseRewrite: %v", err)
		}
		if r.Type() != ResponseBodyJSON {
			t.Errorf("type = %v, want ResponseBodyJSON", r.Type())
		}
		if r.expr == nil {
			t.Fatal("expr program is nil")
		}
		if len(r.payload) != 0 {
			t.Errorf("payload should be cleared for JSON types, got %v", r.payload)
		}
	})

	t.Run("bad url regex", func(t *testing.T) {
		if _, err := ParseRewrite(`(?P<unclosed url reject`); err == nil {
			t.Fatal("expected error for bad url regex")
		}
	})

	t.Run("bad match regex", func(t *testing.T) {
		if _, err := ParseRewrite(`^https?://example\.com url request-header ([bad request-header $1: x`); err == nil {
			t.Fatal("expected error for bad match regex")
		}
	})

	t.Run("bad expr payload", func(t *testing.T) {
		if _, err := ParseRewrite(`^https?://example\.com url json-response-body data.foo ==== 1`); err == nil {
			t.Fatal("expected error for bad expr payload")
		}
	})

	t.Run("missing url keyword", func(t *testing.T) {
		if _, err := ParseRewrite(`^https?://example\.com reject`); err == nil {
			t.Fatal("expected error for missing url keyword")
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		if _, err := ParseRewrite(`^https?://example\.com url whatever`); err == nil {
			t.Fatal("expected error for unknown type")
		}
	})

	t.Run("json type without payload", func(t *testing.T) {
		if _, err := ParseRewrite(`^https?://example\.com url json-response-body`); err == nil {
			t.Fatal("expected error for json type without payload")
		}
	})
}

func TestNewRules(t *testing.T) {
	t.Run("error carries line number", func(t *testing.T) {
		lines := []string{
			`^https?://a\.com url reject`,
			`^https?://b\.com url bogus`,
		}
		_, err := NewRules(lines)
		if err == nil || !strings.Contains(err.Error(), "rewrite rule 1:") {
			t.Fatalf("err = %v, want line number 1", err)
		}
	})

	t.Run("bucketing and empty", func(t *testing.T) {
		rs, err := NewRules([]string{
			`^https?://a\.com url reject`,
			`^https?://b\.com url request-header ^(X-A): request-header X-A: b`,
			`^https?://c\.com url response-body old response-body new`,
		})
		if err != nil {
			t.Fatalf("NewRules: %v", err)
		}
		if rs.Empty() {
			t.Fatal("rules should not be empty")
		}
		if len(rs.request) != 2 {
			t.Errorf("request bucket = %d, want 2", len(rs.request))
		}
		if len(rs.response) != 1 {
			t.Errorf("response bucket = %d, want 1", len(rs.response))
		}

		empty, err := NewRules(nil)
		if err != nil {
			t.Fatalf("NewRules(nil): %v", err)
		}
		if !empty.Empty() {
			t.Error("nil rules should be empty")
		}
	})
}

func TestHandleRequestReject200(t *testing.T) {
	rs, err := NewRules([]string{`^https?://ads\.example\.com url reject-200 {"code":0}`})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://ads.example.com/pixel?id=1", nil)
	rec := httptest.NewRecorder()
	shortCircuit := rs.HandleRequest(rec, req)

	if !shortCircuit {
		t.Fatal("reject-200 should short-circuit")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want json", ct)
	}
	if body := rec.Body.String(); body != `{"code":0}` {
		t.Errorf("body = %q", body)
	}
}

func TestHandleRequestRedirect302(t *testing.T) {
	rs, err := NewRules([]string{`^https?://example\.com/(.*) url 302 https://example.org/$1`})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo/bar", nil)
	rec := httptest.NewRecorder()
	if !rs.HandleRequest(rec, req) {
		t.Fatal("302 should short-circuit")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.org/foo/bar" {
		t.Errorf("location = %q", loc)
	}
}

func TestHandleRequestHeaderCRLF(t *testing.T) {
	// The serialized header block joins fields with CRLF; the match regex
	// spans the Host and User-Agent headers across that boundary and
	// injects a new header via the captured CRLF.
	rs, err := NewRules([]string{
		`^https?://example\.com url request-header (Host):[^\r\n]+(\r\n) request-header $1: rewritten.example.com$2X-Mitm: 1$2`,
	})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://example.com/upload", strings.NewReader("hello"))
	req.Header.Set("Host", "example.com")
	req.Header.Set("User-Agent", "test-agent")
	req.ContentLength = 5

	rec := httptest.NewRecorder()
	if rs.HandleRequest(rec, req) {
		t.Fatal("request-header must not short-circuit")
	}
	if got := req.Header.Get("Host"); got != "rewritten.example.com" {
		t.Errorf("Host = %q, want rewritten.example.com", got)
	}
	if got := req.Header.Get("X-Mitm"); got != "1" {
		t.Errorf("X-Mitm = %q, want injected", got)
	}
	if got := req.Header.Get("User-Agent"); got != "test-agent" {
		t.Errorf("User-Agent = %q, want unchanged", got)
	}
}

func TestHandleRequestNoMatch(t *testing.T) {
	rs, err := NewRules([]string{`^https?://other\.example\.com url reject-200 blocked`})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	rec := httptest.NewRecorder()
	if rs.HandleRequest(rec, req) {
		t.Error("no match should return false")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("no match should not write a response, status = %d", rec.Code)
	}
}

func TestHandleResponseBody(t *testing.T) {
	rs, err := NewRules([]string{`^https?://example\.com url response-body foo response-body bar`})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	resp := &http.Response{
		Request:       httptest.NewRequest(http.MethodGet, "https://example.com/page", nil),
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader("hello foo world foo")),
		ContentLength: -1,
	}
	if err := rs.HandleResponse(resp); err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := string(data); got != "hello bar world bar" {
		t.Errorf("body = %q, want rewritten", got)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "19" {
		t.Errorf("Content-Length = %q, want 19", cl)
	}
}

func TestHandleResponseBodyGzip(t *testing.T) {
	rs, err := NewRules([]string{`^https?://example\.com url response-body foo response-body bar`})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	if _, err := w.Write([]byte("hello foo world")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	resp := &http.Response{
		Request: httptest.NewRequest(http.MethodGet, "https://example.com/page", nil),
		Header: http.Header{
			"Content-Type":     []string{"text/html"},
			"Content-Encoding": []string{"gzip"},
		},
		Body:          io.NopCloser(bytes.NewReader(gz.Bytes())),
		ContentLength: int64(gz.Len()),
	}
	if err := rs.HandleResponse(resp); err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}

	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Errorf("Content-Encoding = %q, want deleted", ce)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := string(data); got != "hello bar world" {
		t.Errorf("body = %q, want rewritten plain", got)
	}
	if resp.ContentLength != int64(len(data)) {
		t.Errorf("ContentLength = %d, want %d", resp.ContentLength, len(data))
	}
	if cl := resp.Header.Get("Content-Length"); cl != "15" {
		t.Errorf("Content-Length header = %q, want 15", cl)
	}
}

func TestExprDelete(t *testing.T) {
	rs, err := NewRules([]string{`^https?://api\.example\.com url json-response-body data.track == "x" && Delete("data.track;data.nested.inner")`})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	origin := map[string]any{
		"track":  "x",
		"keep":   "yes",
		"nested": map[string]any{"inner": float64(1)},
	}
	body, err := json.Marshal(origin)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := &http.Response{
		Request: httptest.NewRequest(http.MethodGet, "https://api.example.com/v1", nil),
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    io.NopCloser(bytes.NewReader(body)),
	}
	if err := rs.HandleResponse(resp); err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := out["track"]; ok {
		t.Error("track should be deleted")
	}
	nested, ok := out["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested = %v, want object", out["nested"])
	}
	if _, ok := nested["inner"]; ok {
		t.Error("nested.inner should be deleted")
	}
	if out["keep"] != "yes" {
		t.Errorf("keep = %v, want untouched", out["keep"])
	}
}

func TestEnvDeleteArray(t *testing.T) {
	env := Env{Data: map[string]any{
		"items": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
	}}
	// Removing every element reports false (nothing kept); removing a
	// subset reports true and keeps the rest.
	if !env.DeleteArray("data.items", "id", "a") {
		t.Error("DeleteArray should report removal")
	}
	left := env.Data["items"].([]any)
	if len(left) != 1 || left[0].(map[string]any)["id"] != "b" {
		t.Errorf("items = %v, want only id b", left)
	}

	// String values are split on ";;" into an any-of list.
	env2 := Env{Data: map[string]any{
		"items": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
	}}
	if env2.DeleteArray("data.items", "id", "a;;b") {
		t.Error("removing every element should report false")
	}
}

func TestHandleResponseNoMatch(t *testing.T) {
	rs, err := NewRules([]string{`^https?://other\.example\.com url response-body foo response-body bar`})
	if err != nil {
		t.Fatalf("NewRules: %v", err)
	}

	resp := &http.Response{
		Request: httptest.NewRequest(http.MethodGet, "https://example.com/page", nil),
		Header:  http.Header{"Content-Type": []string{"text/html"}},
		Body:    io.NopCloser(strings.NewReader("foo")),
	}
	if err := rs.HandleResponse(resp); err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "foo" {
		t.Errorf("body = %q, want unchanged", data)
	}
}
