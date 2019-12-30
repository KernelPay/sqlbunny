// Package strmangle is a collection of string manipulation functions.
// Primarily used by bunny and templates for code generation.
// Because it is focused on pipelining inside templates
// you will see some odd parameter ordering.
package strmangle

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	idAlphabet    = []byte("abcdefghijklmnopqrstuvwxyz")
	smartQuoteRgx = regexp.MustCompile(`^(?i)"?[a-z_][_a-z0-9]*"?(\."?[_a-z][_a-z0-9]*"?)*(\.\*)?$`)
)

var uppercaseWords = map[string]struct{}{
	"acl":   {},
	"api":   {},
	"ascii": {},
	"cpu":   {},
	"eof":   {},
	"guid":  {},
	"id":    {},
	"ip":    {},
	"json":  {},
	"ram":   {},
	"sla":   {},
	"udp":   {},
	"ui":    {},
	"uid":   {},
	"uuid":  {},
	"uri":   {},
	"url":   {},
	"utf8":  {},
	"iban":  {},
}

var reservedWords = map[string]struct{}{
	"break":       {},
	"case":        {},
	"chan":        {},
	"const":       {},
	"continue":    {},
	"default":     {},
	"defer":       {},
	"else":        {},
	"fallthrough": {},
	"for":         {},
	"func":        {},
	"go":          {},
	"goto":        {},
	"if":          {},
	"import":      {},
	"interface":   {},
	"map":         {},
	"package":     {},
	"range":       {},
	"return":      {},
	"select":      {},
	"struct":      {},
	"switch":      {},
	"type":        {},
	"var":         {},
}

func init() {
	// Our Bunny inflection Ruleset does not include uncounmodel inflections.
	// This way, people using words like Sheep will not have
	// collisions with their model name (Sheep) and their
	// function name (Sheep()). Instead, it will
	// use the regular inflection rules: Sheep, Sheeps().
	bunnyRuleset = newBunnyRuleset()
}

// SchemaModel returns a model name with a schema prefixed if
// using a database that supports real schemas, for example,
// for Postgres: "schema_name"."model_name",
// for MS SQL: [schema_name].[model_name], versus
// simply "model_name" for MySQL (because it does not support real schemas)
func SchemaModel(lq, rq string, model string) string {
	return fmt.Sprintf(`%s%s%s`, lq, model, rq)
}

// IdentQuote attempts to quote simple identifiers in SQL statements
func IdentQuote(lq byte, rq byte, s string) string {
	if strings.ToLower(s) == "null" || s == "?" {
		return s
	}

	if m := smartQuoteRgx.MatchString(s); m != true {
		return s
	}

	buf := GetBuffer()
	defer PutBuffer(buf)

	splits := strings.Split(s, ".")
	for i, split := range splits {
		if i != 0 {
			buf.WriteByte('.')
		}

		if split[0] == lq || split[len(split)-1] == rq || split == "*" {
			buf.WriteString(split)
			continue
		}

		buf.WriteByte(lq)
		buf.WriteString(split)
		buf.WriteByte(rq)
	}

	return buf.String()
}

// IdentQuoteSlice applies IdentQuote to a slice.
func IdentQuoteSlice(lq byte, rq byte, s []string) []string {
	if len(s) == 0 {
		return s
	}

	strs := make([]string, len(s))
	for i, str := range s {
		strs[i] = IdentQuote(lq, rq, str)
	}

	return strs
}

// QuoteCharacter returns a string that allows the quote character
// to be embedded into a Go string that uses double quotes:
func QuoteCharacter(q byte) string {
	if q == '"' {
		return `\"`
	}

	return string(q)
}

// Plural converts singular words to plural words (eg: person to people)
func Plural(name string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	splits := strings.Split(name, "_")

	for i := 0; i < len(splits); i++ {
		if i != 0 {
			buf.WriteByte('_')
		}

		if i == len(splits)-1 {
			buf.WriteString(bunnyRuleset.Pluralize(splits[len(splits)-1]))
			break
		}

		buf.WriteString(splits[i])
	}

	return buf.String()
}

// Singular converts plural words to singular words (eg: people to person)
func Singular(name string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	splits := strings.Split(name, "_")

	for i := 0; i < len(splits); i++ {
		if i != 0 {
			buf.WriteByte('_')
		}

		if i == len(splits)-1 {
			buf.WriteString(bunnyRuleset.Singularize(splits[len(splits)-1]))
			break
		}

		buf.WriteString(splits[i])
	}

	return buf.String()
}

// titleCaseCache holds the mapping of title cases.
// Example: map["MyWord"] == "my_word"
var (
	mut            sync.RWMutex
	titleCaseCache = map[string]string{}
)

// TitleCase changes a snake-case variable name
// into a go styled object variable name of "ColumnName".
// titleCase also fully uppercases "ID" components of names, for example
// "field_name_id" to "ColumnNameID".
//
// Note: This method is ugly because it has been highly optimized,
// we found that it was a fairly large bottleneck when we were using regexp.
func TitleCase(n string) string {
	// Attempt to fetch from cache
	mut.RLock()
	val, ok := titleCaseCache[n]
	mut.RUnlock()
	if ok {
		return val
	}

	ln := len(n)
	name := []byte(n)
	buf := GetBuffer()

	start := 0
	end := 0
	for start < ln {
		// Find the start and end of the underscores to account
		// for the possibility of being multiple underscores in a row.
		if end < ln {
			if name[start] == '_' {
				start++
				end++
				continue
				// Once we have found the end of the underscores, we can
				// find the end of the first full word.
			} else if name[end] != '_' {
				end++
				continue
			}
		}

		word := name[start:end]
		wordLen := len(word)
		var vowels bool

		numStart := wordLen
		for i, c := range word {
			vowels = vowels || (c == 97 || c == 101 || c == 105 || c == 111 || c == 117 || c == 121)

			if c > 47 && c < 58 && numStart == wordLen {
				numStart = i
			}
		}

		_, match := uppercaseWords[string(word[:numStart])]

		if match || !vowels {
			// Uppercase all a-z characters
			for _, c := range word {
				if c > 96 && c < 123 {
					buf.WriteByte(c - 32)
				} else {
					buf.WriteByte(c)
				}
			}
		} else {
			if c := word[0]; c > 96 && c < 123 {
				buf.WriteByte(word[0] - 32)
				buf.Write(word[1:])
			} else {
				buf.Write(word)
			}
		}

		start = end + 1
		end = start
	}

	ret := buf.String()
	PutBuffer(buf)

	// Cache the title case result
	mut.Lock()
	titleCaseCache[n] = ret
	mut.Unlock()

	return ret
}

// CamelCase takes a variable name in the format of "var_name" and converts
// it into a go styled variable name of "varName".
// camelCase also fully uppercases "ID" components of names, for example
// "var_name_id" to "varNameID".
func CamelCase(name string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	index := -1
	for i := 0; i < len(name); i++ {
		if name[i] != '_' {
			index = i
			break
		}
	}

	if index != -1 {
		name = name[index:]
	} else {
		return ""
	}

	index = -1
	for i := 0; i < len(name); i++ {
		if name[i] == '_' {
			index = i
			break
		}
	}

	if index == -1 {
		buf.WriteString(name)
	} else {
		buf.WriteString(name[:index])
		buf.WriteString(TitleCase(name[index+1:]))
	}

	return buf.String()
}

// TitleCaseIdentifier splits on dots and then titlecases each fragment.
// map titleCase (split c ".")
func TitleCaseIdentifier(id string) string {
	nextDot := strings.Index(id, "__")
	if nextDot < 0 {
		return TitleCase(id)
	}

	buf := GetBuffer()
	defer PutBuffer(buf)
	lastDot := 0
	ln := len(id)
	addDots := false

	for i := 0; nextDot >= 0; i++ {
		fragment := id[lastDot:nextDot]

		titled := TitleCase(fragment)

		if addDots {
			buf.WriteByte('.')
		}
		buf.WriteString(titled)
		addDots = true

		if nextDot == ln {
			break
		}

		lastDot = nextDot + 2
		if nextDot = strings.Index(id[lastDot:], "__"); nextDot >= 0 {
			nextDot += lastDot
		} else {
			nextDot = ln
		}
	}

	return buf.String()
}

// MakeStringMap converts a map[string]string into the format:
// "key": "value", "key": "value"
func MakeStringMap(types map[string]string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	c := 0
	for _, k := range keys {
		v := types[k]
		buf.WriteString(fmt.Sprintf("`%s`: `%s`", k, v))
		if c < len(types)-1 {
			buf.WriteString(", ")
		}

		c++
	}

	return buf.String()
}

// StringMap maps a function over a slice of strings.
func StringMap(modifier func(string) string, strs []string) []string {
	ret := make([]string, len(strs))

	for i, str := range strs {
		ret[i] = modifier(str)
	}

	return ret
}

// PrefixStringSlice with the given str.
func PrefixStringSlice(str string, strs []string) []string {
	ret := make([]string, len(strs))

	for i, s := range strs {
		ret[i] = fmt.Sprintf("%s%s", str, s)
	}

	return ret
}

// Placeholders generates the SQL statement placeholders for in queries.
// For example, ($1,$2,$3),($4,$5,$6) etc.
// It will start counting placeholders at "start".
// If indexPlaceholders is false, it will convert to ? instead of $1 etc.
func Placeholders(indexPlaceholders bool, count int, start int, group int) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	if start == 0 || group == 0 {
		panic("Invalid start or group numbers supplied.")
	}

	if group > 1 {
		buf.WriteByte('(')
	}
	for i := 0; i < count; i++ {
		if i != 0 {
			if group > 1 && i%group == 0 {
				buf.WriteString("),(")
			} else {
				buf.WriteByte(',')
			}
		}
		if indexPlaceholders {
			buf.WriteString(fmt.Sprintf("$%d", start+i))
		} else {
			buf.WriteByte('?')
		}
	}
	if group > 1 {
		buf.WriteByte(')')
	}

	return buf.String()
}

// SetParamNames takes a slice of fields and returns a comma separated
// list of parameter names for a template statement SET clause.
// eg: "col1"=$1, "col2"=$2, "col3"=$3
func SetParamNames(lq, rq string, start int, fields []string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	for i, c := range fields {
		if start != 0 {
			buf.WriteString(fmt.Sprintf(`%s%s%s=$%d`, lq, c, rq, i+start))
		} else {
			buf.WriteString(fmt.Sprintf(`%s%s%s=?`, lq, c, rq))
		}

		if i < len(fields)-1 {
			buf.WriteByte(',')
		}
	}

	return buf.String()
}

// WhereClause returns the where clause using start as the $ flag index
// For example, if start was 2 output would be: "colthing=$2 AND colstuff=$3"
func WhereClause(lq, rq string, start int, cols []string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	for i, c := range cols {
		if start != 0 {
			buf.WriteString(fmt.Sprintf(`%s%s%s=$%d`, lq, c, rq, start+i))
		} else {
			buf.WriteString(fmt.Sprintf(`%s%s%s=?`, lq, c, rq))
		}

		if i < len(cols)-1 {
			buf.WriteString(" AND ")
		}
	}

	return buf.String()
}

// WhereClauseRepeated returns the where clause repeated with OR clause using start as the $ flag index
// For example, if start was 2 output would be: "(colthing=$2 AND colstuff=$3) OR (colthing=$4 AND colstuff=$5)"
func WhereClauseRepeated(lq, rq string, start int, cols []string, count int) string {
	var startIndex int
	buf := GetBuffer()
	defer PutBuffer(buf)
	buf.WriteByte('(')
	for i := 0; i < count; i++ {
		if i != 0 {
			buf.WriteString(") OR (")
		}

		startIndex = 0
		if start > 0 {
			startIndex = start + i*len(cols)
		}

		buf.WriteString(WhereClause(lq, rq, startIndex, cols))
	}
	buf.WriteByte(')')

	return buf.String()
}

// JoinOnClause returns a join on clause
func JoinOnClause(lq, rq string, table1 string, cols1 []string, table2 string, cols2 []string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	for i := range cols1 {
		c1 := cols1[i]
		c2 := cols2[i]
		buf.WriteString(fmt.Sprintf(
			`%s%s%s.%s%s%s=%s%s%s.%s%s%s`,
			lq, table1, rq, lq, c1, rq,
			lq, table2, rq, lq, c2, rq,
		))

		if i < len(cols1)-1 {
			buf.WriteString(" AND ")
		}
	}

	return buf.String()
}

// JoinWhereClause returns a where clause explicitly specifying the table name
func JoinWhereClause(lq, rq string, start int, table string, cols []string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	for i, c := range cols {
		if start != 0 {
			buf.WriteString(fmt.Sprintf(`%s%s%s.%s%s%s=$%d`, lq, table, rq, lq, c, rq, start+i))
		} else {
			buf.WriteString(fmt.Sprintf(`%s%s%s.%s%s%s=?`, lq, table, rq, lq, c, rq))
		}

		if i < len(cols)-1 {
			buf.WriteString(" AND ")
		}
	}

	return buf.String()
}

// WhereClause returns the where clause using start as the $ flag index
// For example, if start was 2 output would be: "colthing=$2 AND colstuff=$3"
func WhereInClause(lq, rq string, table string, cols []string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	if len(cols) != 1 {
		buf.WriteString("(")
	}
	for i, c := range cols {
		buf.WriteString(fmt.Sprintf(`%s%s%s.%s%s%s`, lq, table, rq, lq, c, rq))

		if i < len(cols)-1 {
			buf.WriteString(",")
		}
	}
	if len(cols) != 1 {
		buf.WriteString(")")
	}

	return buf.String()
}

// JoinSlices merges two string slices of equal length
func JoinSlices(sep string, a, b []string) []string {
	lna, lnb := len(a), len(b)
	if lna != lnb {
		panic("joinSlices: can only merge slices of same length")
	} else if lna == 0 {
		return nil
	}

	ret := make([]string, len(a))
	for i, elem := range a {
		ret[i] = fmt.Sprintf("%s%s%s", elem, sep, b[i])
	}

	return ret
}

// StringSliceMatch returns true if the length of both
// slices is the same, and the elements of both slices are the same.
// The elements can be in any order.
func StringSliceMatch(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for _, aval := range a {
		found := false
		for _, bval := range b {
			if bval == aval {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// ContainsAny returns true if any of the passed in strings are
// found in the passed in string slice
func ContainsAny(a []string, finds ...string) bool {
	for _, s := range a {
		for _, find := range finds {
			if s == find {
				return true
			}
		}
	}

	return false
}

// GenerateIgnoreTags converts a slice of tag strings into
// ignore tags that can be passed onto the end of a struct, for example:
// tags: ["xml", "db"] convert to: xml:"-" db:"-"
func GenerateIgnoreTags(tags []string) string {
	buf := GetBuffer()
	defer PutBuffer(buf)

	for _, tag := range tags {
		buf.WriteString(tag)
		buf.WriteString(`:"-" `)
	}

	return buf.String()
}

// ReplaceReservedWords takes a word and replaces it with word_ if it's found
// in the list of reserved words.
func ReplaceReservedWords(word string) string {
	if _, ok := reservedWords[word]; ok {
		return word + "_"
	}
	return word
}
