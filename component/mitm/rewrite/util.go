package rewrite

import (
	"strings"

	"github.com/dlclark/regexp2"
)

// allowContentType lists media type prefixes whose bodies may be rewritten.
var allowContentType = []string{
	"text/",
	"application/xhtml",
	"application/xml",
	"application/xhtml+xml",
	"application/atom+xml",
	"application/json",
	"application/x-www-form-urlencoded",
}

func canRewriteBody(mediaType string) bool {
	if mediaType == "" {
		return false
	}

	for _, v := range allowContentType {
		if strings.HasPrefix(mediaType, v) {
			return true
		}
	}

	return false
}

// findStringSubmatch collects all capture groups across all matches of re
// in s. Index 0 is the first full match, mirroring the semantics the
// payload $N expansion relies on.
func findStringSubmatch(re *regexp2.Regexp, s string) []string {
	var sub []string
	m, _ := re.FindStringMatch(s)
	for m != nil {
		for _, g := range m.Groups() {
			for _, c := range g.Captures {
				sub = append(sub, c.String())
			}
		}
		m, _ = re.FindNextMatch(m)
	}
	return sub
}
