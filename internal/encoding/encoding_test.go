package encoding_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/encoding"
)

func TestLookupResolvesNamesAndAliases(t *testing.T) {
	for _, name := range []string{"windows-1250", "cp1252", "ISO-8859-2", "utf-8"} {
		t.Run(name, func(t *testing.T) {
			dec, err := encoding.Lookup(name)
			require.NoError(t, err)
			require.Equal(t, name, dec.Name())
		})
	}
}

// The IANA registry has no "cpNNNN" alias, but that is the spelling operators
// reach for and the one the config docs advertise, so it must resolve.
func TestLookupAcceptsCPCodePageSpelling(t *testing.T) {
	for _, name := range []string{"cp1250", "cp1252", "CP1251"} {
		dec, err := encoding.Lookup(name)
		require.NoError(t, err, "charset %q must resolve", name)
		require.Equal(t, name, dec.Name(), "the declared spelling is reported back")
	}

	// cp1252 and windows-1252 must resolve to the same decoding.
	viaAlias, err := encoding.Lookup("cp1252")
	require.NoError(t, err)
	viaName, err := encoding.Lookup("windows-1252")
	require.NoError(t, err)
	a, err := viaAlias.Decode([]byte{0x93})
	require.NoError(t, err)
	b, err := viaName.Decode([]byte{0x93})
	require.NoError(t, err)
	require.Equal(t, string(b), string(a))
}

func TestLookupRejectsUnusableCharsets(t *testing.T) {
	for _, name := range []string{"", "   ", "not-a-real-charset"} {
		_, err := encoding.Lookup(name)
		require.Error(t, err, "charset %q must not resolve", name)
	}
}

func TestDecodeConvertsLegacyBytes(t *testing.T) {
	dec, err := encoding.Lookup("windows-1250")
	require.NoError(t, err)

	// 0xE2 is "â" in Windows-1250 and an invalid lone lead byte in UTF-8.
	out, err := dec.Decode([]byte("C\xE2mpul"))
	require.NoError(t, err)
	require.Equal(t, "Câmpul", string(out))
}

// The same bytes decode differently under a different charset. This is why a
// charset is declared rather than guessed: both decodes "succeed".
func TestDecodeIsCharsetDependent(t *testing.T) {
	cp1250, err := encoding.Lookup("windows-1250")
	require.NoError(t, err)
	cp1252, err := encoding.Lookup("cp1252")
	require.NoError(t, err)

	raw := []byte{0xBA}
	a, err := cp1250.Decode(raw)
	require.NoError(t, err)
	b, err := cp1252.Decode(raw)
	require.NoError(t, err)
	require.NotEqual(t, string(a), string(b))
}

func TestDecodeOutputIsAlwaysValidUTF8(t *testing.T) {
	dec, err := encoding.Lookup("windows-1250")
	require.NoError(t, err)

	out, err := dec.Decode([]byte{0xE2, 0xBA, 0xFE, 0x41})
	require.NoError(t, err)
	require.True(t, len(out) > 0)
	require.Equal(t, out, []byte(string(out)))
}

func TestZeroDecoderIsUnusable(t *testing.T) {
	var dec encoding.Decoder
	_, err := dec.Decode([]byte("x"))
	require.Error(t, err)
}
