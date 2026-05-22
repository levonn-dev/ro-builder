package hercules

import "fmt"

type tokenType int

const (
	tokEOF tokenType = iota
	tokIdent
	tokString
	tokInt
	tokBool
	tokLBrace
	tokRBrace
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokColon
	tokComma
	tokHeredoc
)

func (tt tokenType) String() string {
	switch tt {
	case tokEOF:
		return "EOF"
	case tokIdent:
		return "ident"
	case tokString:
		return "string"
	case tokInt:
		return "int"
	case tokBool:
		return "bool"
	case tokLBrace:
		return "{"
	case tokRBrace:
		return "}"
	case tokLParen:
		return "("
	case tokRParen:
		return ")"
	case tokLBracket:
		return "["
	case tokRBracket:
		return "]"
	case tokColon:
		return ":"
	case tokComma:
		return ","
	case tokHeredoc:
		return "heredoc"
	}
	return fmt.Sprintf("token(%d)", int(tt))
}

type token struct {
	typ  tokenType
	val  string
	line int
	col  int
}

func (t token) String() string {
	if t.val == "" {
		return fmt.Sprintf("%s at %d:%d", t.typ, t.line, t.col)
	}
	return fmt.Sprintf("%s(%q) at %d:%d", t.typ, t.val, t.line, t.col)
}

// tokenizer is a single-token-lookahead tokenizer for Hercules's libconfig
// dialect. The position counters are 1-based for human-readable errors.
type tokenizer struct {
	src    []byte
	pos    int
	line   int
	col    int
	peeked *token
}

func newTokenizer(src []byte) *tokenizer {
	return &tokenizer{src: src, line: 1, col: 1}
}

func (t *tokenizer) advance(n int) {
	for i := 0; i < n && t.pos < len(t.src); i++ {
		if t.src[t.pos] == '\n' {
			t.line++
			t.col = 1
		} else {
			t.col++
		}
		t.pos++
	}
}

func (t *tokenizer) peekChar(offset int) byte {
	if t.pos+offset >= len(t.src) {
		return 0
	}
	return t.src[t.pos+offset]
}

func (t *tokenizer) peek() (token, error) {
	if t.peeked != nil {
		return *t.peeked, nil
	}
	tk, err := t.read()
	if err != nil {
		return token{}, err
	}
	t.peeked = &tk
	return tk, nil
}

func (t *tokenizer) next() (token, error) {
	if t.peeked != nil {
		tk := *t.peeked
		t.peeked = nil
		return tk, nil
	}
	return t.read()
}

func (t *tokenizer) expect(typ tokenType) (token, error) {
	tk, err := t.next()
	if err != nil {
		return token{}, err
	}
	if tk.typ != typ {
		return token{}, fmt.Errorf("expected %s, got %s", typ, tk)
	}
	return tk, nil
}

func (t *tokenizer) skipWhitespaceAndComments() {
	for t.pos < len(t.src) {
		ch := t.src[t.pos]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			t.advance(1)
		case ch == '/' && t.peekChar(1) == '/':
			for t.pos < len(t.src) && t.src[t.pos] != '\n' {
				t.advance(1)
			}
		case ch == '/' && t.peekChar(1) == '*':
			t.advance(2)
			for t.pos < len(t.src) {
				if t.src[t.pos] == '*' && t.peekChar(1) == '/' {
					t.advance(2)
					break
				}
				t.advance(1)
			}
		default:
			return
		}
	}
}

func (t *tokenizer) read() (token, error) {
	t.skipWhitespaceAndComments()
	if t.pos >= len(t.src) {
		return token{typ: tokEOF, line: t.line, col: t.col}, nil
	}
	line, col := t.line, t.col
	ch := t.src[t.pos]
	switch ch {
	case '{':
		t.advance(1)
		return token{typ: tokLBrace, line: line, col: col}, nil
	case '}':
		t.advance(1)
		return token{typ: tokRBrace, line: line, col: col}, nil
	case '(':
		t.advance(1)
		return token{typ: tokLParen, line: line, col: col}, nil
	case ')':
		t.advance(1)
		return token{typ: tokRParen, line: line, col: col}, nil
	case '[':
		t.advance(1)
		return token{typ: tokLBracket, line: line, col: col}, nil
	case ']':
		t.advance(1)
		return token{typ: tokRBracket, line: line, col: col}, nil
	case ':':
		t.advance(1)
		return token{typ: tokColon, line: line, col: col}, nil
	case ',':
		t.advance(1)
		return token{typ: tokComma, line: line, col: col}, nil
	case ';':
		// libconfig's other separator. mob_db.conf uses commas exclusively,
		// but skill_tree.conf trails its inherit list with ';'. Treat as a
		// comma synonym so we don't add a new token type / parser branch.
		t.advance(1)
		return token{typ: tokComma, line: line, col: col}, nil
	case '"':
		return t.readString(line, col)
	case '<':
		if t.peekChar(1) == '"' {
			return t.readHeredoc(line, col)
		}
		return token{}, fmt.Errorf("unexpected '<' at line %d col %d", line, col)
	}
	if ch == '-' {
		return t.readInt(line, col)
	}
	if ch >= '0' && ch <= '9' {
		// Could be a number or a digit-prefixed identifier. Hercules's
		// mob_db Drops blocks use item AegisNames as keys, and some begin
		// with digits ("3rd_Floor_Pass: 1000", "1000Year_Old_Tree: 50").
		// Disambiguate by lookahead: digits followed by a letter/underscore
		// is an identifier, otherwise it's a plain int.
		return t.readIntOrDigitIdent(line, col)
	}
	if isIdentStart(ch) {
		return t.readIdent(line, col)
	}
	return token{}, fmt.Errorf("unexpected character %q at line %d col %d", string(ch), line, col)
}

func (t *tokenizer) readString(line, col int) (token, error) {
	t.advance(1) // opening "
	var sb []byte
	for t.pos < len(t.src) {
		ch := t.src[t.pos]
		if ch == '\\' && t.pos+1 < len(t.src) {
			nxt := t.src[t.pos+1]
			t.advance(2)
			switch nxt {
			case 'n':
				sb = append(sb, '\n')
			case 't':
				sb = append(sb, '\t')
			case 'r':
				sb = append(sb, '\r')
			case '"':
				sb = append(sb, '"')
			case '\\':
				sb = append(sb, '\\')
			default:
				sb = append(sb, nxt)
			}
			continue
		}
		if ch == '"' {
			t.advance(1)
			return token{typ: tokString, val: string(sb), line: line, col: col}, nil
		}
		sb = append(sb, ch)
		t.advance(1)
	}
	return token{}, fmt.Errorf("unterminated string starting at line %d col %d", line, col)
}

// readHeredoc reads Hercules's <" ... "> block literally, preserving every
// byte between the delimiters (including newlines). Embedded ", " and >
// are fine on their own; only the exact closing sequence "> ends the block.
func (t *tokenizer) readHeredoc(line, col int) (token, error) {
	t.advance(2) // consume <"
	var sb []byte
	for t.pos < len(t.src) {
		if t.src[t.pos] == '"' && t.peekChar(1) == '>' {
			t.advance(2)
			return token{typ: tokHeredoc, val: string(sb), line: line, col: col}, nil
		}
		sb = append(sb, t.src[t.pos])
		t.advance(1)
	}
	return token{}, fmt.Errorf("unterminated heredoc starting at line %d col %d", line, col)
}

func (t *tokenizer) readInt(line, col int) (token, error) {
	start := t.pos
	if t.src[t.pos] == '-' {
		t.advance(1)
	}
	for t.pos < len(t.src) && t.src[t.pos] >= '0' && t.src[t.pos] <= '9' {
		t.advance(1)
	}
	return token{typ: tokInt, val: string(t.src[start:t.pos]), line: line, col: col}, nil
}

// readIntOrDigitIdent reads a sequence starting with a digit. If the
// digit run is followed by an ident-continuation character (letter or
// underscore), the whole run is an identifier (Hercules's mob-drop maps
// use AegisNames like "3rd_Floor_Pass" as keys); otherwise it's a plain
// integer literal.
func (t *tokenizer) readIntOrDigitIdent(line, col int) (token, error) {
	start := t.pos
	for t.pos < len(t.src) && t.src[t.pos] >= '0' && t.src[t.pos] <= '9' {
		t.advance(1)
	}
	if t.pos < len(t.src) && isIdentStart(t.src[t.pos]) {
		for t.pos < len(t.src) && isIdentCont(t.src[t.pos]) {
			t.advance(1)
		}
		return token{typ: tokIdent, val: string(t.src[start:t.pos]), line: line, col: col}, nil
	}
	return token{typ: tokInt, val: string(t.src[start:t.pos]), line: line, col: col}, nil
}

func (t *tokenizer) readIdent(line, col int) (token, error) {
	start := t.pos
	for t.pos < len(t.src) && isIdentCont(t.src[t.pos]) {
		t.advance(1)
	}
	val := string(t.src[start:t.pos])
	if val == "true" || val == "false" {
		return token{typ: tokBool, val: val, line: line, col: col}, nil
	}
	return token{typ: tokIdent, val: val, line: line, col: col}, nil
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentCont(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}
