package rewrite

import (
	"fmt"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// payloadSeparator splits multiple regexes or payloads within one segment.
const payloadSeparator = "<and>"

// ParseRewrite parses a single rewrite rule line:
//
//	^https?://... url <type> [match] [<type>] [payload]
//
// The type word delimits the match pattern from the replacement payload
// (e.g. "url request-header MATCH request-header REPLACE"); a single
// segment is a payload only (e.g. "url 302 TARGET"). Both segments may
// carry multiple values joined by payloadSeparator. JSON types compile
// their payload into an expr program.
func ParseRewrite(line string) (*Rule, error) {
	url, others, found := strings.Cut(strings.TrimSpace(line), "url")
	if !found {
		return nil, fmt.Errorf("invalid rewrite rule: missing url keyword")
	}

	urlPattern, err := regexp2.Compile(strings.TrimSpace(url), regexp2.None)
	if err != nil {
		return nil, fmt.Errorf("invalid url regex %q: %w", strings.TrimSpace(url), err)
	}

	others = strings.TrimSpace(others)
	first, _, _ := strings.Cut(others, " ")

	var (
		typ     Type
		match   []*regexp2.Regexp
		payload []string
	)
	for t := Reject; t <= ResponseBodyJSON; t++ {
		k := t.String()
		if k == others {
			// bare type word, e.g. "url reject"
			typ = t
			break
		}
		if k != first {
			continue
		}

		// Split the remainder on the type word: one segment is a payload,
		// two segments are match patterns and payloads.
		rs := trimArr(strings.Split(others, k))
		switch l := len(rs); l {
		case 1:
			typ = t
			payload = trimArr(strings.Split(rs[0], payloadSeparator))
		case 2:
			for _, str := range trimArr(strings.Split(rs[0], payloadSeparator)) {
				regx, err := regexp2.Compile(str, regexp2.None)
				if err != nil {
					return nil, fmt.Errorf("invalid match regex %q: %w", str, err)
				}
				match = append(match, regx)
			}
			typ = t
			payload = trimArr(strings.Split(rs[1], payloadSeparator))
		}
		break
	}
	if typ == 0 {
		return nil, fmt.Errorf("invalid rewrite rule: unknown type %q", others)
	}

	var prog *vm.Program
	if jsonTypes[typ] {
		if len(payload) == 0 {
			return nil, fmt.Errorf("invalid rewrite rule: type %s requires an expr payload", typ)
		}
		prog, err = expr.Compile(payload[0], expr.Env(Env{}), expr.AsBool())
		if err != nil {
			return nil, fmt.Errorf("failed to compile rewrite rule. type: %s, url: %s, error: %w", typ, urlPattern, err)
		}
		payload = nil
	}

	return &Rule{
		urlPattern: urlPattern,
		typ:        typ,
		match:      match,
		payload:    payload,
		expr:       prog,
	}, nil
}

// trimArr drops empty entries and trims spaces.
func trimArr(arr []string) (r []string) {
	for _, e := range arr {
		if s := strings.TrimSpace(e); s != "" {
			r = append(r, s)
		}
	}
	return
}
