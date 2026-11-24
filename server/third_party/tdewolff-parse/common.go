// Package parse contains a collection of parsers for various formats in its subpackages.
package parse

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strconv"
)

var (
	dataSchemeBytes = []byte("data:")
	base64Bytes     = []byte("base64")
	textMimeBytes   = []byte("text/plain")
)

// ErrBadDataURI is returned by DataURI when the byte slice does not start with 'data:' or is too short.
var ErrBadDataURI = errors.New("not a data URI")

// Number returns the number of bytes that parse as a number of the regex format (+|-)?([0-9]+(\.[0-9]+)?|\.[0-9]+)((e|E)(+|-)?[0-9]+)?.
func Number(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	i := 0
	if b[i] == '+' || b[i] == '-' {
		i++
		if i >= len(b) {
			return 0
		}
	}
	firstDigit := (b[i] >= '0' && b[i] <= '9')
	if firstDigit {
		i++
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
	}
	if i < len(b) && b[i] == '.' {
		i++
		if i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
			for i < len(b) && b[i] >= '0' && b[i] <= '9' {
				i++
			}
		} else if firstDigit {
			// . could belong to the next token
			i--
			return i
		} else {
			return 0
		}
	} else if !firstDigit {
		return 0
	}
	iOld := i
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		i++
		if i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		if i >= len(b) || b[i] < '0' || b[i] > '9' {
			// e could belong to next token
			return iOld
		}
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
	}
	return i
}

// Dimension parses a byte-slice and returns the length of the number and its unit.
func Dimension(b []byte) (int, int) {
	num := Number(b)
	if num == 0 || num == len(b) {
		return num, 0
	} else if b[num] == '%' {
		return num, 1
	} else if b[num] >= 'a' && b[num] <= 'z' || b[num] >= 'A' && b[num] <= 'Z' {
		i := num + 1
		for i < len(b) && (b[i] >= 'a' && b[i] <= 'z' || b[i] >= 'A' && b[i] <= 'Z') {
			i++
		}
		return num, i - num
	}
	return num, 0
}

// Mediatype parses a given mediatype and splits the mimetype from the parameters.
// It works similar to mime.ParseMediaType but is faster.
func Mediatype(b []byte) ([]byte, map[string]string) {
	i := 0
	for i < len(b) && b[i] == ' ' {
		i++
	}
	b = b[i:]
	n := len(b)
	mimetype := b
	var params map[string]string
	for i := 3; i < n; i++ { // mimetype is at least three characters long
		if b[i] == ';' || b[i] == ' ' {
			mimetype = b[:i]
			if b[i] == ' ' {
				i++ // space
				for i < n && b[i] == ' ' {
					i++
				}
				if n <= i || b[i] != ';' {
					break
				}
			}
			params = map[string]string{}
			s := string(b)
		PARAM:
			i++ // semicolon
			for i < n && s[i] == ' ' {
				i++
			}
			start := i
			for i < n && s[i] != '=' && s[i] != ';' && s[i] != ' ' {
				i++
			}
			key := s[start:i]
			for i < n && s[i] == ' ' {
				i++
			}
			if i < n && s[i] == '=' {
				i++
				for i < n && s[i] == ' ' {
					i++
				}
				start = i
				for i < n && s[i] != ';' && s[i] != ' ' {
					i++
				}
			} else {
				start = i
			}
			params[key] = s[start:i]
			for i < n && s[i] == ' ' {
				i++
			}
			if i < n && s[i] == ';' {
				goto PARAM
			}
			break
		}
	}
	return mimetype, params
}

// DataURI parses the given data URI and returns the mediatype, data and ok.
func DataURI(dataURI []byte) ([]byte, []byte, error) {
	if len(dataURI) > 5 && bytes.Equal(dataURI[:5], dataSchemeBytes) {
		dataURI = dataURI[5:]
		inBase64 := false
		var mediatype []byte
		i := 0
		for j := 0; j < len(dataURI); j++ {
			c := dataURI[j]
			if c == '=' || c == ';' || c == ',' {
				if c != '=' && bytes.Equal(TrimWhitespace(dataURI[i:j]), base64Bytes) {
					if len(mediatype) > 0 {
						mediatype = mediatype[:len(mediatype)-1]
					}
					inBase64 = true
					i = j
				} else if c != ',' {
					mediatype = append(append(mediatype, TrimWhitespace(dataURI[i:j])...), c)
					i = j + 1
				} else {
					mediatype = append(mediatype, TrimWhitespace(dataURI[i:j])...)
				}
				if c == ',' {
					if len(mediatype) == 0 || mediatype[0] == ';' {
						mediatype = textMimeBytes
					}
					data := dataURI[j+1:]
					if inBase64 {
						decoded := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
						n, err := base64.StdEncoding.Decode(decoded, data)
						if err != nil {
							return nil, nil, err
						}
						data = decoded[:n]
					} else {
						data = DecodeURL(data)
					}
					return mediatype, data, nil
				}
			}
		}
	}
	return nil, nil, ErrBadDataURI
}

// QuoteEntity parses the given byte slice and returns the quote that got matched (' or ") and its entity length.
// TODO: deprecated
func QuoteEntity(b []byte) (quote byte, n int) {
	if len(b) < 5 || b[0] != '&' {
		return 0, 0
	}
	if b[1] == '#' {
		if b[2] == 'x' {
			i := 3
			for i < len(b) && b[i] == '0' {
				i++
			}
			if i+2 < len(b) && b[i] == '2' && b[i+2] == ';' {
				if b[i+1] == '2' {
					return '"', i + 3 // &#x22;
				} else if b[i+1] == '7' {
					return '\'', i + 3 // &#x27;
				}
			}
		} else {
			i := 2
			for i < len(b) && b[i] == '0' {
				i++
			}
			if i+2 < len(b) && b[i] == '3' && b[i+2] == ';' {
				if b[i+1] == '4' {
					return '"', i + 3 // &#34;
				} else if b[i+1] == '9' {
					return '\'', i + 3 // &#39;
				}
			}
		}
	} else if len(b) >= 6 && b[5] == ';' {
		if bytes.Equal(b[1:5], []byte{'q', 'u', 'o', 't'}) {
			return '"', 6 // &quot;
		} else if bytes.Equal(b[1:5], []byte{'a', 'p', 'o', 's'}) {
			return '\'', 6 // &apos;
		}
	}
	return 0, 0
}

// ReplaceMultipleWhitespace replaces character series of space, \n, \t, \f, \r into a single space or newline (when the serie contained a \n or \r).
func ReplaceMultipleWhitespace(b []byte) []byte {
	j, k := 0, 0 // j is write position, k is start of next text section
	for i := 0; i < len(b); i++ {
		if IsWhitespace(b[i]) {
			start := i
			newline := IsNewline(b[i])
			i++
			for ; i < len(b) && IsWhitespace(b[i]); i++ {
				if IsNewline(b[i]) {
					newline = true
				}
			}
			if newline {
				b[start] = '\n'
			} else {
				b[start] = ' '
			}
			if 1 < i-start { // more than one whitespace
				if j == 0 {
					j = start + 1
				} else {
					j += copy(b[j:], b[k:start+1])
				}
				k = i
			}
		}
	}
	if j == 0 {
		return b
	} else if j == 1 { // only if starts with whitespace
		b[k-1] = b[0]
		return b[k-1:]
	} else if k < len(b) {
		j += copy(b[j:], b[k:])
	}
	return b[:j]
}

// replaceEntities will replace in b at index i, assuming that b[i] == '&' and that i+3<len(b). The returned int will be the last character of the entity, so that the next iteration can safely do i++ to continue and not miss any entitites.
func replaceEntities(b []byte, i int, entitiesMap map[string][]byte, revEntitiesMap map[byte][]byte) ([]byte, int) {
	const MaxEntityLength = 31 // longest HTML entity: CounterClockwiseContourIntegral
	var r []byte
	j := i + 1
	if b[j] == '#' {
		j++
		if b[j] == 'x' {
			j++
			c := 0
			for ; j < len(b) && (b[j] >= '0' && b[j] <= '9' || b[j] >= 'a' && b[j] <= 'f' || b[j] >= 'A' && b[j] <= 'F'); j++ {
				if b[j] <= '9' {
					c = c<<4 + int(b[j]-'0')
				} else if b[j] <= 'F' {
					c = c<<4 + int(b[j]-'A') + 10
				} else if b[j] <= 'f' {
					c = c<<4 + int(b[j]-'a') + 10
				}
			}
			if j <= i+3 || 10000 <= c {
				return b, j - 1
			}
			if c < 128 {
				r = []byte{byte(c)}
			} else {
				r = append(r, '&', '#')
				r = strconv.AppendInt(r, int64(c), 10)
				r = append(r, ';')
			}
		} else {
			c := 0
			for ; j < len(b) && c < 128 && b[j] >= '0' && b[j] <= '9'; j++ {
				c = c*10 + int(b[j]-'0')
			}
			if j <= i+2 || 128 <= c {
				return b, j - 1
			}
			r = []byte{byte(c)}
		}
	} else {
		for ; j < len(b) && j-i-1 <= MaxEntityLength && b[j] != ';'; j++ {
			if !(b[j] >= '0' && b[j] <= '9' || b[j] >= 'a' && b[j] <= 'z' || b[j] >= 'A' && b[j] <= 'Z') {
				// invalid character reference character
				break
			}
		}
		if len(b) <= j || j == i+1 || b[j] != ';' {
			return b, i
		}

		var ok bool
		r, ok = entitiesMap[string(b[i+1:j])]
		if !ok {
			return b, j
		}
	}

	// j is at semicolon
	n := j + 1 - i
	if j < len(b) && b[j] == ';' && 2 < n {
		if len(r) == 1 {
			if q, ok := revEntitiesMap[r[0]]; ok {
				if len(q) == len(b[i:j+1]) && bytes.Equal(q, b[i:j+1]) {
					return b, j
				}
				r = q
			} else if r[0] == '&' {
				// check if for example &amp; is followed by something that could potentially be an entity
				k := j + 1
				if k < len(b) && (b[k] >= '0' && b[k] <= '9' || b[k] >= 'a' && b[k] <= 'z' || b[k] >= 'A' && b[k] <= 'Z' || b[k] == '#') {
					return b, k
				}
			}
		}

		copy(b[i:], r)
		copy(b[i+len(r):], b[j+1:])
		b = b[:len(b)-n+len(r)]
		return b, i + len(r) - 1
	}
	return b, i
}

// ReplaceEntities replaces all occurrences of entites (such as &quot;) to their respective unencoded bytes.
func ReplaceEntities(b []byte, entitiesMap map[string][]byte, revEntitiesMap map[byte][]byte) []byte {
	for i := 0; i < len(b); i++ {
		if b[i] == '&' && i+3 < len(b) {
			b, i = replaceEntities(b, i, entitiesMap, revEntitiesMap)
		}
	}
	return b
}

// ReplaceMultipleWhitespaceAndEntities is a combination of ReplaceMultipleWhitespace and ReplaceEntities. It is faster than executing both sequentially.
func ReplaceMultipleWhitespaceAndEntities(b []byte, entitiesMap map[string][]byte, revEntitiesMap map[byte][]byte) []byte {
	j, k := 0, 0 // j is write position, k is start of next text section
	for i := 0; i < len(b); i++ {
		if IsWhitespace(b[i]) {
			start := i
			newline := IsNewline(b[i])
			i++
			for ; i < len(b) && IsWhitespace(b[i]); i++ {
				if IsNewline(b[i]) {
					newline = true
				}
			}
			if newline {
				b[start] = '\n'
			} else {
				b[start] = ' '
			}
			if 1 < i-start { // more than one whitespace
				if j == 0 {
					j = start + 1
				} else {
					j += copy(b[j:], b[k:start+1])
				}
				k = i
			}
		}
		if i+3 < len(b) && b[i] == '&' {
			b, i = replaceEntities(b, i, entitiesMap, revEntitiesMap)
		}
	}
	if j == 0 {
		return b
	} else if j == 1 { // only if starts with whitespace
		b[k-1] = b[0] // move newline to end of whitespace
		return b[k-1:]
	} else if k < len(b) {
		j += copy(b[j:], b[k:])
	}
	return b[:j]
}

// URLEncodingTable is a charmap for which characters need escaping in the URL encoding scheme
var URLEncodingTable = [256]bool{
	// ASCII
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, false, true, true, true, true, true, false, // space, ", #, $, %, &
	false, false, false, true, true, false, false, true, // +, comma, /
	false, false, false, false, false, false, false, false,
	false, false, true, true, true, true, true, true, // :, ;, <, =, >, ?

	true, false, false, false, false, false, false, false, // @
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, true, true, true, true, false, // [, \, ], ^

	true, false, false, false, false, false, false, false, // `
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, true, true, true, false, true, // {, |, }, DEL

	// non-ASCII
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
}

// DataURIEncodingTable is a charmap for which characters need escaping in the Data URI encoding scheme
// Escape only non-printable characters, unicode and %, #, &.
// IE11 additionally requires encoding of \, [, ], ", <, >, `, {, }, |, ^ which is not required by Chrome, Firefox, Opera, Edge, Safari, Yandex
// To pass the HTML validator, restricted URL characters must be escaped: non-printable characters, space, <, >, #, %, "
var DataURIEncodingTable = [256]bool{
	// ASCII
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, false, true, true, false, true, true, false, // space, ", #, %, &
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, true, false, true, false, // <, >

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, true, true, true, true, false, // [, \, ], ^

	true, false, false, false, false, false, false, false, // `
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, true, true, true, false, true, // {, |, }, DEL

	// non-ASCII
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,

	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
	true, true, true, true, true, true, true, true,
}

// EncodeURL encodes bytes using the URL encoding scheme
func EncodeURL(b []byte, table [256]bool) []byte {
	for i := 0; i < len(b); i++ {
		c := b[i]
		if table[c] {
			b = append(b, 0, 0)
			copy(b[i+3:], b[i+1:])
			b[i+0] = '%'
			b[i+1] = "0123456789ABCDEF"[c>>4]
			b[i+2] = "0123456789ABCDEF"[c&15]
		}
	}
	return b
}

// DecodeURL decodes an URL encoded using the URL encoding scheme
func DecodeURL(b []byte) []byte {
	for i := 0; i < len(b); i++ {
		if b[i] == '%' && i+2 < len(b) {
			j := i + 1
			c := 0
			for ; j < i+3 && (b[j] >= '0' && b[j] <= '9' || b[j] >= 'a' && b[j] <= 'f' || b[j] >= 'A' && b[j] <= 'F'); j++ {
				if b[j] <= '9' {
					c = c<<4 + int(b[j]-'0')
				} else if b[j] <= 'F' {
					c = c<<4 + int(b[j]-'A') + 10
				} else if b[j] <= 'f' {
					c = c<<4 + int(b[j]-'a') + 10
				}
			}
			if j == i+3 {
				b[i] = byte(c)
				b = append(b[:i+1], b[i+3:]...)
			}
		} else if b[i] == '+' {
			b[i] = ' '
		}
	}
	return b
}

func AppendEscape(b, str, chars []byte, escape byte) []byte {
	i := 0
	for j := 0; j < len(str); j++ {
		has := false
		for _, c := range chars {
			if c == str[j] {
				has = true
				break
			}
		}
		if has || str[j] == escape {
			if i < j {
				b = append(b, str[i:j]...)
			}
			b = append(b, escape)
			i = j
		}
	}
	if i < len(str) {
		b = append(b, str[i:]...)
	}
	return b
}
