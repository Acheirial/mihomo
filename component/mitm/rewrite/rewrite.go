package rewrite

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/expr-lang/expr/vm"
)

// Type is the rewrite action of a rule.
type Type int

const (
	Reject Type = iota + 1 // 404
	Reject200
	Reject204
	RejectImg
	RejectDict
	RejectArray
	Redirect302
	Redirect307
	RequestHeader
	RequestHeaderJSON
	RequestBody
	RequestBodyJSON
	ResponseHeader
	ResponseHeaderJSON
	ResponseBody
	ResponseBodyJSON
)

func (t Type) String() string {
	switch t {
	case Reject:
		return "reject" // 404
	case Reject200:
		return "reject-200"
	case Reject204:
		return "reject-204"
	case RejectImg:
		return "reject-img"
	case RejectDict:
		return "reject-dict"
	case RejectArray:
		return "reject-array"
	case Redirect302:
		return "302"
	case Redirect307:
		return "307"
	case RequestHeader:
		return "request-header"
	case RequestHeaderJSON:
		return "json-request-header"
	case RequestBody:
		return "request-body"
	case RequestBodyJSON:
		return "json-request-body"
	case ResponseHeader:
		return "response-header"
	case ResponseHeaderJSON:
		return "json-response-header"
	case ResponseBody:
		return "response-body"
	case ResponseBodyJSON:
		return "json-response-body"
	default:
		return "Unknown"
	}
}

// typeMapping maps rule word forms ("url reject-200", "url 302 TARGET",
// "url request-header MATCH REPLACE", "url json-response-body EXPR") to
// rewrite types.
var typeMapping = func() map[string]Type {
	m := make(map[string]Type, 16)
	for i := Reject; i <= ResponseBodyJSON; i++ {
		m[i.String()] = i
	}
	return m
}()

// jsonTypes are the rule types whose payload is an expr program instead of
// a literal replacement.
var jsonTypes = map[Type]bool{
	RequestHeaderJSON:  true,
	RequestBodyJSON:    true,
	ResponseHeaderJSON: true,
	ResponseBodyJSON:   true,
}

// Rule is a single parsed rewrite rule.
type Rule struct {
	urlPattern *regexp2.Regexp
	typ        Type
	match      []*regexp2.Regexp
	payload    []string
	expr       *vm.Program
}

func (r *Rule) Type() Type {
	return r.typ
}

// replaceURLPayload expands $1..$N in the redirect target with URL
// submatches.
func (r *Rule) replaceURLPayload(matchSub []string) string {
	if len(r.payload) == 0 {
		return ""
	}
	url := r.payload[0]
	l := len(matchSub)
	if l < 2 {
		return url
	}
	for i := 1; i < l; i++ {
		url = strings.ReplaceAll(url, "$"+strconv.Itoa(i), matchSub[i])
	}
	return url
}

// Rules holds rewrite rules split into request-side and response-side
// buckets. Bucketing by rule type happens in NewRules.
type Rules struct {
	request  []*Rule
	response []*Rule
}

// NewRules parses lines into rules; errors carry the line number.
func NewRules(lines []string) (*Rules, error) {
	r := &Rules{}
	for i, line := range lines {
		rule, err := ParseRewrite(line)
		if err != nil {
			return nil, fmt.Errorf("rewrite rule %d: %w", i, err)
		}
		if rule.typ <= RequestBodyJSON {
			r.request = append(r.request, rule)
		} else {
			r.response = append(r.response, rule)
		}
	}
	return r, nil
}

// Empty reports whether no rule was parsed.
func (r *Rules) Empty() bool {
	return len(r.request) == 0 && len(r.response) == 0
}

// replaceSubPayload applies each match pattern to oldData, replacing the
// full match with the payload after $1..$N expansion of submatches. Reports
// whether any pattern matched.
func (r *Rule) replaceSubPayload(oldData string) (string, bool) {
	if r.match == nil || r.payload == nil {
		return oldData, false
	}

	var (
		ok      bool
		payload string
	)
	for i, pl := 0, len(r.payload); i < len(r.match); i++ {
		if i < pl {
			payload = r.payload[i]
		}

		sub := findStringSubmatch(r.match[i], oldData)
		l := len(sub)
		if l == 0 {
			continue
		}

		ok = true
		newPayload := payload
		for j := 1; j < l; j++ {
			newPayload = strings.ReplaceAll(newPayload, "$"+strconv.Itoa(j), sub[j])
		}

		oldData = strings.ReplaceAll(oldData, sub[0], newPayload)
	}

	return oldData, ok
}
