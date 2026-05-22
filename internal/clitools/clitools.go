// Package clitools holds small helpers shared across the cmd/ tools so the
// same `find go.mod and walk up` and `within edit distance 1` logic doesn't
// have to be cloned per tool. Internal-only; the API binary doesn't depend
// on it at runtime.
package clitools

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindRepoRoot walks up from the current working directory looking for
// go.mod and returns the first directory that contains one. Returns an
// error if no go.mod is found before reaching the filesystem root.
func FindRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			return cwd, nil
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", fmt.Errorf("could not find go.mod walking up from %s", cwd)
		}
		cwd = parent
	}
}

// Levenshtein1 returns true iff a and b are within edit distance 1; a
// single insertion, deletion, or substitution. Tighter and faster than the
// general DP table since we only need the boolean. Operates on runes so
// multi-byte UTF-8 characters (accented names, kRO transliterations
// occasionally present in rAthena data) compare as one logical character
// instead of N bytes; `len(s)` and `s[i]` would give byte counts and
// produce wrong results for non-ASCII names.
func Levenshtein1(a, b string) bool {
	ar := []rune(a)
	br := []rune(b)
	la, lb := len(ar), len(br)
	if la == lb {
		diff := 0
		for i := 0; i < la; i++ {
			if ar[i] != br[i] {
				diff++
				if diff > 1 {
					return false
				}
			}
		}
		return true
	}
	if la-lb == 1 || lb-la == 1 {
		shorter, longer := ar, br
		if la > lb {
			shorter, longer = br, ar
		}
		i, j := 0, 0
		var skipped bool
		for i < len(shorter) && j < len(longer) {
			if shorter[i] != longer[j] {
				if skipped {
					return false
				}
				skipped = true
				j++
				continue
			}
			i++
			j++
		}
		return true
	}
	return false
}
