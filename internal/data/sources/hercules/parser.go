// Package hercules parses Hercules's libconfig-style database files.
//
// Hercules ships its database (db/pre-re/, db/re/) in a libconfig dialect
// with one extension: heredoc-style script blocks delimited by `<"` and `">`,
// which can span multiple lines. The grammar otherwise:
//
//	root := IDENT ":" value
//	value := INT | STRING | BOOL | HEREDOC | object | list | IDENT
//	object := "{" (IDENT ":" value [","])* "}"
//	list := "(" (value [","])* ")"
//
// Comments use `//` to end of line and `/* ... */` block style. Field
// separators inside objects are optional whitespace (commas accepted but
// not required); list elements use commas.
//
// ParseFile returns the top-level list as []any. Object values are
// map[string]any; primitives are int / string / bool. Heredoc bodies become
// plain strings (we don't interpret the embedded Hercules script DSL).
package hercules

import (
	"fmt"
	"strconv"
)

func ParseFile(src []byte) ([]any, error) {
	t := newTokenizer(src)
	rootName, err := t.next()
	if err != nil {
		return nil, err
	}
	if rootName.typ != tokIdent {
		return nil, fmt.Errorf("expected root identifier, got %s", rootName)
	}
	if _, err := t.expect(tokColon); err != nil {
		return nil, err
	}
	val, err := parseValue(t)
	if err != nil {
		return nil, err
	}
	list, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("expected top-level list under %q, got %T", rootName.val, val)
	}
	// Verify nothing trailing.
	tk, err := t.next()
	if err != nil {
		return nil, err
	}
	if tk.typ != tokEOF {
		return nil, fmt.Errorf("unexpected trailing token %s", tk)
	}
	return list, nil
}

func parseValue(t *tokenizer) (any, error) {
	tk, err := t.peek()
	if err != nil {
		return nil, err
	}
	// next() after a successful peek() can't fail; the buffered token is
	// already validated. Discard the result explicitly so errcheck stays
	// useful for genuine unchecked-error sites elsewhere.
	switch tk.typ {
	case tokInt:
		_, _ = t.next()
		n, err := strconv.Atoi(tk.val)
		if err != nil {
			return nil, fmt.Errorf("bad int %q at line %d col %d: %w", tk.val, tk.line, tk.col, err)
		}
		return n, nil
	case tokString, tokHeredoc:
		_, _ = t.next()
		return tk.val, nil
	case tokBool:
		_, _ = t.next()
		return tk.val == "true", nil
	case tokIdent:
		// Bare identifiers as values are uncommon (Hercules quotes IT_WEAPON
		// etc). Treat as string for tolerance.
		_, _ = t.next()
		return tk.val, nil
	case tokLBrace:
		return parseObject(t)
	case tokLParen:
		return parseList(t)
	case tokLBracket:
		return parseArray(t)
	}
	return nil, fmt.Errorf("unexpected %s while parsing value", tk)
}

func parseObject(t *tokenizer) (map[string]any, error) {
	if _, err := t.expect(tokLBrace); err != nil {
		return nil, err
	}
	obj := make(map[string]any)
	for {
		tk, err := t.peek()
		if err != nil {
			return nil, err
		}
		if tk.typ == tokRBrace {
			_, _ = t.next()
			return obj, nil
		}
		if tk.typ == tokComma {
			_, _ = t.next()
			continue
		}
		if tk.typ != tokIdent {
			return nil, fmt.Errorf("expected key or '}', got %s", tk)
		}
		key, _ := t.next()
		if _, err := t.expect(tokColon); err != nil {
			return nil, err
		}
		val, err := parseValue(t)
		if err != nil {
			return nil, err
		}
		obj[key.val] = val
	}
}

// parseArray reads `[ value, value, ... ]`. Hercules uses arrays for
// fields that hold a small homogeneous list; Loc when an item occupies
// multiple slots ("EQP_HEAD_TOP", "EQP_HEAD_MID"), EquipLv as [min, max],
// Stack as [amount, type]. Returns []any like parseList does.
func parseArray(t *tokenizer) ([]any, error) {
	if _, err := t.expect(tokLBracket); err != nil {
		return nil, err
	}
	var items []any
	for {
		tk, err := t.peek()
		if err != nil {
			return nil, err
		}
		if tk.typ == tokRBracket {
			_, _ = t.next()
			return items, nil
		}
		if tk.typ == tokComma {
			_, _ = t.next()
			continue
		}
		val, err := parseValue(t)
		if err != nil {
			return nil, err
		}
		items = append(items, val)
	}
}

func parseList(t *tokenizer) ([]any, error) {
	if _, err := t.expect(tokLParen); err != nil {
		return nil, err
	}
	var items []any
	for {
		tk, err := t.peek()
		if err != nil {
			return nil, err
		}
		if tk.typ == tokRParen {
			_, _ = t.next()
			return items, nil
		}
		if tk.typ == tokComma {
			_, _ = t.next()
			continue
		}
		val, err := parseValue(t)
		if err != nil {
			return nil, err
		}
		items = append(items, val)
	}
}
