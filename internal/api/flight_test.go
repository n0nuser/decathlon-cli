package api

import (
	"strings"
	"testing"
)

func pushes(chunks ...string) []byte {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(`<script>self.__next_f.push([1,"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(c) + `"])</script>`)
	}
	return []byte(b.String())
}

// Text rows carry a byte length and no newline; the next row follows right
// after. Splitting on newlines alone lost the row after every text row,
// which is where the product's own item group sat.
func TestFlightTextRows(t *testing.T) {
	text := "ñandú" // multi-byte: the length counts bytes, not runes
	body := pushes("1:I[1,[],\"\"]\n", "2:T7,"+text+"3:{\"want\":1,\"price\":\"$undefined\"}\n4:[\"x\"]\n")
	f, err := ParseFlight(body)
	if err != nil {
		t.Fatal(err)
	}
	hits := f.Find(func(m map[string]any) bool { return has(m, "want") })
	if len(hits) != 1 {
		t.Fatalf("row after a text row was lost: %d", len(hits))
	}
	if v, ok := hits[0]["price"]; !ok || v != nil {
		t.Fatalf("$undefined should decode to null, got %#v", v)
	}
}

func TestFlightMissing(t *testing.T) {
	_, err := ParseFlight([]byte("<html>Just a moment...</html>"))
	if e, ok := err.(*Error); !ok || e.Kind != KindAPIChanged {
		t.Fatalf("got %v", err)
	}
}

func TestFold(t *testing.T) {
	if fold("  Tienda CAMPAÑA ") != "tienda campana" || slug("Nº de plazas") != "no-de-plazas" {
		t.Fatal(fold("Tienda CAMPAÑA"), slug("Nº de plazas"))
	}
}
