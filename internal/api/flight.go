package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

// Flight is the React Server Components payload a Next.js App Router page
// streams inline through self.__next_f.push([1,"..."]) calls. The storefront
// renders everything the CLI needs (listings, facets, product groups, stores)
// from it, so it is the API of record: no client-side JSON endpoint is called.
//
// Rows look like `id:payload`. Most payloads are one JSON value ended by a
// newline; text rows are `id:T<hexlen>,<bytes>` with a byte length and no
// trailing newline, so the next row starts right after them.
type Flight struct {
	rows []any
}

var pushRE = regexp.MustCompile(`self\.__next_f\.push\(\[1,"((?:[^"\\]|\\.)*)"\]\)`)

// ParseFlight extracts and decodes the flight rows of a page. A page with no
// flight data is reported as an API change: every storefront page has some.
func ParseFlight(html []byte) (*Flight, error) {
	var stream bytes.Buffer
	for _, m := range pushRE.FindAllSubmatch(html, -1) {
		var chunk string
		quoted := append(append([]byte{'"'}, m[1]...), '"')
		if err := json.Unmarshal(quoted, &chunk); err != nil {
			return nil, fmt.Errorf("flight chunk: %w", err)
		}
		stream.WriteString(chunk)
	}
	if stream.Len() == 0 {
		return nil, apiChanged("page carries no next.js flight data")
	}
	return parseRows(stream.Bytes()), nil
}

// undefinedToken is how React serialises `undefined`. It shows up where
// numbers, bools and objects are expected, so it becomes null before decoding.
var undefinedToken = []byte(`"$undefined"`)

func parseRows(b []byte) *Flight {
	f := &Flight{}
	for i := 0; i < len(b); {
		colon := bytes.IndexByte(b[i:], ':')
		if colon < 0 {
			break
		}
		i += colon + 1
		if i < len(b) && b[i] == 'T' {
			comma := bytes.IndexByte(b[i:], ',')
			if comma < 0 {
				break
			}
			n, err := strconv.ParseInt(string(b[i+1:i+comma]), 16, 64)
			if err != nil {
				break
			}
			i += comma + 1 + int(n)
			continue
		}
		end := bytes.IndexByte(b[i:], '\n')
		if end < 0 {
			end = len(b) - i
		}
		body := b[i : i+end]
		i += end + 1
		if len(body) == 0 || (body[0] != '[' && body[0] != '{') {
			continue // module references, hints, errors: not data
		}
		var v any
		if json.Unmarshal(bytes.ReplaceAll(body, undefinedToken, []byte("null")), &v) == nil {
			f.rows = append(f.rows, v)
		}
	}
	return f
}

// Find returns every object in the payload for which match is true, in
// document order. Row ids change from page to page, so data is located by
// its own keys rather than by where it sits.
func (f *Flight) Find(match func(map[string]any) bool) []map[string]any {
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if match(t) {
				out = append(out, t)
			}
			for _, c := range t {
				walk(c)
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	for _, r := range f.rows {
		walk(r)
	}
	return out
}

// decodeInto re-marshals a found object into a typed struct.
func decodeInto(obj map[string]any, dst any) error {
	raw, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func has(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
