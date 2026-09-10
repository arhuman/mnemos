// Package encoding resolves legacy text charset names and decodes their bytes
// to UTF-8. It exists so the config loader can validate a declared charset at
// startup and the ingest pipeline can apply the same resolution at read time,
// without either package importing the other.
package encoding

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

// Decoder converts a legacy-encoded byte slice to UTF-8.
type Decoder struct {
	name string
	enc  encoding.Encoding
}

// Lookup resolves a charset name or alias (e.g. "windows-1250", "cp1252",
// "ISO-8859-2") through the IANA index. It returns an error for an empty,
// unknown, or unsupported name so a bad declaration is caught at config load
// rather than per-file mid-ingest. UTF-8 resolves to a Decoder whose Decode is
// an identity check, which keeps an explicit `charset = "utf-8"` rule legal.
func Lookup(name string) (Decoder, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Decoder{}, errors.New("encoding: empty charset")
	}

	enc, err := ianaindex.IANA.Encoding(canonicalName(trimmed))
	if err != nil {
		return Decoder{}, fmt.Errorf("encoding: unknown charset %q: %w", name, err)
	}
	// IANA.Encoding reports a nil Encoding with a nil error for names it knows
	// but cannot decode; without this the failure would surface as a nil-deref at
	// the first matching file instead of at config load.
	if enc == nil {
		return Decoder{}, fmt.Errorf("encoding: unsupported charset %q", name)
	}

	return Decoder{name: trimmed, enc: enc}, nil
}

// canonicalName maps the common "cpNNNN" spelling of the Windows code pages to
// the registered "windows-NNNN" name. The IANA registry has no cpNNNN alias, so
// without this the spelling operators most often reach for is rejected. Every
// other name is passed through untouched and resolved by the index itself.
func canonicalName(name string) string {
	rest, ok := strings.CutPrefix(strings.ToLower(name), "cp")
	if !ok {
		return name
	}
	switch rest {
	case "1250", "1251", "1252", "1253", "1254", "1255", "1256", "1257", "1258":
		return "windows-" + rest
	}

	return name
}

// Name returns the charset name the Decoder was resolved from.
func (d Decoder) Name() string { return d.name }

// Decode converts content from the Decoder's charset to UTF-8. It returns an
// error when the bytes are not valid in that charset, so a mis-declared rule
// surfaces as a per-file skip rather than as silently mangled text. The result
// is guaranteed valid UTF-8.
func (d Decoder) Decode(content []byte) ([]byte, error) {
	if d.enc == nil {
		return nil, errors.New("encoding: zero Decoder")
	}

	out, err := d.enc.NewDecoder().Bytes(content)
	if err != nil {
		return nil, fmt.Errorf("encoding: decode as %s: %w", d.name, err)
	}
	// A single-byte charmap maps every byte to some rune, so a wrong charset is
	// not caught above. Validity is still asserted because the pipeline's
	// downstream contract (chunking, FTS, the embedder's tokenizer) is UTF-8.
	if !utf8.Valid(out) {
		return nil, fmt.Errorf("encoding: decode as %s produced invalid UTF-8", d.name)
	}

	return out, nil
}
