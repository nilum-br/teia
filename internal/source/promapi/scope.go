package promapi

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Scope holds label values every series a query reads must carry, such as
// cluster="prod-east" on a Mimir, Thanos or VictoriaMetrics that stores
// several clusters. Kiali scopes its queries the same way.
type Scope map[string]string

// Dialect says how Apply finds a query's selectors.
type Dialect int

const (
	// PromQL: every vector selector (metric, metric{...}, {...}).
	PromQL Dialect = iota
	// LogQL: every stream selector ({...}).
	LogQL
)

// legacyName is a label name PromQL takes unquoted.
var legacyName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// String renders the scope as matchers: {cluster="east"}.
func (s Scope) String() string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	slices.Sort(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name + "=" + strconv.Quote(s[name])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// Validate checks the scope's label names and values.
func (s Scope) Validate() error {
	for name, value := range s {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\"\\\n") {
			return fmt.Errorf("selector label %q is not a label name", name)
		}
		if value == "" {
			return fmt.Errorf("selector label %q needs a value", name)
		}
	}
	return nil
}

// aggregations and keywords are identifiers PromQL never reads as metric
// names; aggregations take a parenthesised expression, possibly after a
// by/without clause.
var (
	aggregations = map[string]bool{"sum": true, "min": true, "max": true, "avg": true, "group": true, "stddev": true, "stdvar": true,
		"count": true, "count_values": true, "bottomk": true, "topk": true, "quantile": true, "limitk": true, "limit_ratio": true}
	keywords = map[string]bool{"by": true, "without": true, "on": true, "ignoring": true, "group_left": true, "group_right": true,
		"bool": true, "offset": true, "and": true, "or": true, "unless": true, "atan2": true, "inf": true, "nan": true,
		"start": true, "end": true, "smoothed": true, "anchored": true}
	// labelLists are keywords followed by a parenthesised list of label
	// names, not by an expression.
	labelLists = map[string]bool{"by": true, "without": true, "on": true, "ignoring": true, "group_left": true, "group_right": true}
)

type tokenKind int

const (
	tokOther tokenKind = iota
	tokIdent
	tokString
	tokNumber
	tokOpen  // ( [ {
	tokClose // ) ] }
	tokComma
	tokMatch // = != =~ !~
)

type token struct {
	kind       tokenKind
	text       string
	start, end int // byte offsets in the query
}

// lex splits a PromQL or LogQL query into the tokens Apply needs. Strings,
// numbers and durations are single tokens, so braces inside them are never
// mistaken for selectors; comments are dropped.
func lex(q string) ([]token, error) {
	var out []token
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '#':
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'' || c == '`':
			j := i + 1
			for ; j < len(q) && q[j] != c; j++ {
				if q[j] == '\\' && c != '`' {
					j++
				}
			}
			if j >= len(q) {
				return nil, fmt.Errorf("unterminated string at byte %d", i)
			}
			out = append(out, token{tokString, q[i : j+1], i, j + 1})
			i = j + 1
		case isDigit(c) || (c == '.' && i+1 < len(q) && isDigit(q[i+1])):
			j := i + 1
			for j < len(q) && (isAlnum(q[j]) || q[j] == '.' || ((q[j] == '+' || q[j] == '-') && (q[j-1] == 'e' || q[j-1] == 'E') && !strings.HasPrefix(strings.ToLower(q[i:j]), "0x"))) {
				j++
			}
			out = append(out, token{tokNumber, q[i:j], i, j})
			i = j
		case isAlpha(c) || c == '_' || c == ':':
			j := i + 1
			for j < len(q) && (isAlnum(q[j]) || q[j] == '_' || q[j] == ':') {
				j++
			}
			out = append(out, token{tokIdent, q[i:j], i, j})
			i = j
		case c == '(' || c == '[' || c == '{':
			out = append(out, token{tokOpen, q[i : i+1], i, i + 1})
			i++
		case c == ')' || c == ']' || c == '}':
			out = append(out, token{tokClose, q[i : i+1], i, i + 1})
			i++
		case c == ',':
			out = append(out, token{tokComma, ",", i, i + 1})
			i++
		case (c == '=' || c == '!') && i+1 < len(q) && q[i+1] == '~':
			out = append(out, token{tokMatch, q[i : i+2], i, i + 2})
			i += 2
		case c == '!' && i+1 < len(q) && q[i+1] == '=':
			out = append(out, token{tokMatch, "!=", i, i + 2})
			i += 2
		case c == '=' && i+1 < len(q) && q[i+1] == '=':
			out = append(out, token{tokOther, "==", i, i + 2})
			i += 2
		case c == '=':
			out = append(out, token{tokMatch, "=", i, i + 1})
			i++
		default:
			out = append(out, token{tokOther, q[i : i+1], i, i + 1})
			i++
		}
	}
	return out, nil
}

// balanced checks that every bracket closes the one it should.
func balanced(toks []token) error {
	pairs := map[string]string{")": "(", "]": "[", "}": "{"}
	var stack []token
	for _, t := range toks {
		switch t.kind {
		case tokOpen:
			stack = append(stack, t)
		case tokClose:
			if len(stack) == 0 || stack[len(stack)-1].text != pairs[t.text] {
				return fmt.Errorf("unexpected %q at byte %d", t.text, t.start)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 0 {
		return fmt.Errorf("%q at byte %d is never closed", stack[len(stack)-1].text, stack[len(stack)-1].start)
	}
	return nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) || c == '_' }

// Apply returns query with the scope's matchers added to every selector,
// except where the selector already matches on that label: an explicit
// matcher in the query wins. A query it cannot read is an error, never sent
// unscoped.
func (s Scope) Apply(query string, dialect Dialect) (string, error) {
	if len(s) == 0 {
		return query, nil
	}
	toks, err := lex(query)
	if err != nil {
		return "", err
	}
	if err := balanced(toks); err != nil {
		return "", err
	}
	type insert struct {
		at   int
		text string
	}
	var inserts []insert
	// matchers renders the scope's matchers absent from the selector whose
	// matcher list is toks[open+1:close].
	matchers := func(open, close int) string {
		present := map[string]bool{}
		for k := open + 1; k+1 < close; k++ {
			if (toks[k].kind == tokIdent || toks[k].kind == tokString) && toks[k+1].kind == tokMatch {
				name := toks[k].text
				if toks[k].kind == tokString {
					if u, err := strconv.Unquote(name); err == nil {
						name = u
					}
				}
				present[name] = true
			}
		}
		names := make([]string, 0, len(s))
		for name := range s {
			if !present[name] {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		parts := make([]string, len(names))
		for i, name := range names {
			label := name
			if !legacyName.MatchString(name) {
				label = strconv.Quote(name)
			}
			parts[i] = label + "=" + strconv.Quote(s[name])
		}
		return strings.Join(parts, ",")
	}
	// closing finds the bracket that closes toks[i].
	closing := func(i int) (int, error) {
		depth := 0
		for k := i; k < len(toks); k++ {
			switch toks[k].kind {
			case tokOpen:
				depth++
			case tokClose:
				depth--
				if depth == 0 {
					return k, nil
				}
			}
		}
		return 0, fmt.Errorf("%q at byte %d is never closed", toks[i].text, toks[i].start)
	}
	// selector adds the matchers inside the braces at toks[open].
	selector := func(open int) (int, error) {
		close, err := closing(open)
		if err != nil {
			return 0, err
		}
		m := matchers(open, close)
		switch {
		case m == "":
		case close == open+1:
			inserts = append(inserts, insert{toks[open].end, m})
		case toks[open+1].kind == tokString && toks[open+2].kind != tokMatch:
			// {"metric.name", ...}: the quoted metric name stays first.
			inserts = append(inserts, insert{toks[open+1].end, "," + m})
		default:
			inserts = append(inserts, insert{toks[open].end, m + ","})
		}
		return close, nil
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		next := func() *token {
			if i+1 < len(toks) {
				return &toks[i+1]
			}
			return nil
		}()
		switch {
		case t.kind == tokOpen && t.text == "{":
			close, err := selector(i)
			if err != nil {
				return "", err
			}
			i = close
		case dialect == LogQL:
			// Only braces are stream selectors in LogQL.
		case t.kind == tokOpen && t.text == "[":
			// Ranges, subquery steps and durations hold no selector.
			close, err := closing(i)
			if err != nil {
				return "", err
			}
			i = close
		case t.kind == tokIdent:
			word := strings.ToLower(t.text)
			switch {
			case labelLists[word]:
				if next != nil && next.kind == tokOpen && next.text == "(" {
					close, err := closing(i + 1)
					if err != nil {
						return "", err
					}
					i = close
				}
			case keywords[word] || aggregations[word]:
			case next != nil && next.kind == tokOpen && next.text == "(":
				// A function call: its arguments are read as they come.
			case next != nil && next.kind == tokOpen && next.text == "{":
				close, err := selector(i + 1)
				if err != nil {
					return "", err
				}
				i = close
			default:
				// A bare metric name.
				inserts = append(inserts, insert{t.end, "{" + matchers(i, i) + "}"})
			}
		}
	}
	var b strings.Builder
	last := 0
	for _, in := range inserts {
		b.WriteString(query[last:in.at])
		b.WriteString(in.text)
		last = in.at
	}
	b.WriteString(query[last:])
	return b.String(), nil
}
