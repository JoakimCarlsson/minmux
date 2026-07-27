package clientgen

import (
	"go/token"
	"strconv"
	"strings"
	"unicode"
)

// initialisms are rendered fully upper-case when a wire name is converted
// into a Go identifier, so "created_by_id" becomes "CreatedByID" rather
// than "CreatedById". Keyed by the lower-cased word.
var initialisms = map[string]string{
	"acl":   "ACL",
	"api":   "API",
	"cors":  "CORS",
	"cpu":   "CPU",
	"csv":   "CSV",
	"db":    "DB",
	"dns":   "DNS",
	"eof":   "EOF",
	"etag":  "ETag",
	"guid":  "GUID",
	"html":  "HTML",
	"http":  "HTTP",
	"https": "HTTPS",
	"id":    "ID",
	"ip":    "IP",
	"json":  "JSON",
	"jwt":   "JWT",
	"mime":  "MIME",
	"os":    "OS",
	"ram":   "RAM",
	"rpc":   "RPC",
	"sla":   "SLA",
	"smtp":  "SMTP",
	"sql":   "SQL",
	"sse":   "SSE",
	"ssh":   "SSH",
	"tls":   "TLS",
	"ttl":   "TTL",
	"uri":   "URI",
	"url":   "URL",
	"utf8":  "UTF8",
	"uuid":  "UUID",
	"xml":   "XML",
	"yaml":  "YAML",
}

// exportedName converts a wire name (a JSON property, a query parameter, a
// header) into an exported Go identifier. Separators and case transitions
// are treated as word breaks and recognized initialisms are upper-cased:
//
//	"created_at"    -> "CreatedAt"
//	"X-Request-Id"  -> "XRequestID"
//	"userId"        -> "UserID"
func exportedName(s string) string {
	words := splitWords(s)
	var b strings.Builder
	for _, w := range words {
		if up, ok := initialisms[strings.ToLower(w)]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(capitalizeWord(w))
	}
	return ensureIdentifier(b.String())
}

// typeName converts an OpenAPI component schema name into a Go type name.
//
// Unlike exportedName it does not re-word the input: component names
// produced by minmux are already Go type names, and splitting them on case
// transitions would mangle deliberate spellings such as "OAuth2Config".
// Only invalid characters are stripped and the first rune is exported.
func typeName(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case unicode.IsLetter(r), r == '_':
			b.WriteRune(r)
		case unicode.IsDigit(r) && i > 0:
			b.WriteRune(r)
		}
	}
	return ensureIdentifier(exportFirst(b.String()))
}

// methodName converts an operationId into a Go method name. Ids that are
// already camelCase (minmux derives "getTodosById") only need their first
// rune exported; ids carrying separators are re-worded so an explicit
// openapi.OperationID("list_todos") still yields "ListTodos".
func methodName(id string) string {
	if id == "" {
		return ""
	}
	if strings.ContainsFunc(id, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		return exportedName(id)
	}
	return ensureIdentifier(exportFirst(id))
}

// splitWords breaks a name on separators and on case transitions, keeping
// runs of capitals together: "XMLHttpRequest" -> [XML, Http, Request].
func splitWords(s string) []string {
	var (
		words []string
		cur   strings.Builder
	)
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if i > 0 && unicode.IsUpper(r) {
				prev := runes[i-1]
				nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					flush()
				} else if unicode.IsUpper(prev) && nextLower {
					flush()
				}
			}
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return words
}

func capitalizeWord(w string) string {
	if w == "" {
		return w
	}
	runes := []rune(w)
	runes[0] = unicode.ToUpper(runes[0])
	if allUpper(runes) {
		return string(runes)
	}
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) && i+1 < len(runes) &&
			unicode.IsUpper(runes[i+1]) {
			return string(runes)
		}
	}
	return string(runes)
}

func allUpper(runes []rune) bool {
	for _, r := range runes {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

func exportFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// ensureIdentifier guarantees the result is a legal, non-reserved Go
// identifier: an empty or digit-leading name is prefixed, and a name
// colliding with a keyword is suffixed.
func ensureIdentifier(s string) string {
	if s == "" {
		return "Field"
	}
	if r := []rune(s)[0]; unicode.IsDigit(r) {
		s = "N" + s
	}
	if token.Lookup(s).IsKeyword() {
		s += "_"
	}
	return s
}

// reservedNames are the identifiers the emitted runtime file already
// occupies at package scope. A component schema colliding with one of them
// is renamed rather than allowed to produce uncompilable output.
var reservedNames = map[string]bool{
	"APIError":        true,
	"BaseURL":         true,
	"Client":          true,
	"ErrorValue":      true,
	"FormFile":        true,
	"New":             true,
	"Option":          true,
	"Part":            true,
	"SSEData":         true,
	"SSEEvent":        true,
	"WithHTTPClient":  true,
	"WithHeader":      true,
	"WithRequestHook": true,
	"WithTransport":   true,
}

// nameSet allocates unique Go identifiers, resolving collisions
// deterministically by appending an increasing numeric suffix.
type nameSet struct {
	used map[string]bool
}

func newNameSet(reserved map[string]bool) *nameSet {
	s := &nameSet{used: make(map[string]bool, len(reserved))}
	for k := range reserved {
		s.used[k] = true
	}
	return s
}

func (s *nameSet) claim(want string) string {
	if want == "" {
		want = "Type"
	}
	if !s.used[want] {
		s.used[want] = true
		return want
	}
	for i := 2; ; i++ {
		candidate := want + strconv.Itoa(i)
		if !s.used[candidate] {
			s.used[candidate] = true
			return candidate
		}
	}
}
