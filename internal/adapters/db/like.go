package db

import "strings"

// likeWildcards escapes the three characters LIKE treats as special. The backslash goes first, or escaping the other two would escape the escapes.
var likeWildcards = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsPattern builds the argument for a `col ILIKE ? ESCAPE '\'` containment match.
//
// % and _ are wildcards to LIKE and ordinary characters to whoever typed them into a search box: "100%" has to find "100%" rather than everything that
// starts with 100, and "user_id" must not also match "userXid".
func containsPattern(s string) string {
	return "%" + likeWildcards.Replace(s) + "%"
}
