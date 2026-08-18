package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestNormalizeCLIJSONEncodingAcceptsSupportedPowerShellEncodings(t *testing.T) {
	want := []byte("{\"value\":\"evidence \U0001D11E\"}")
	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "utf8", body: append([]byte(nil), want...)},
		{name: "utf8-bom", body: append(append([]byte(nil), utf8BOM...), want...)},
		{name: "utf16-little-endian", body: encodeTestUTF16(want, true)},
		{name: "utf16-big-endian", body: encodeTestUTF16(want, false)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeCLIJSONEncoding(test.body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("decoded bytes differ:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestNormalizeCLIJSONEncodingRejectsMalformedOrUnsupportedInput(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
		want string
	}{
		{name: "invalid-utf8", body: []byte{0x7b, 0xff, 0x7d}, want: "not valid UTF-8"},
		{name: "utf32-le", body: append(append([]byte(nil), utf32LEBOM...), 0x7b, 0, 0, 0), want: "unsupported UTF-32"},
		{name: "utf32-be", body: append(append([]byte(nil), utf32BEBOM...), 0, 0, 0, 0x7b), want: "unsupported UTF-32"},
		{name: "truncated-utf16", body: append(append([]byte(nil), utf16LEBOM...), 0x7b), want: "truncated UTF-16"},
		{name: "unpaired-high", body: append(append([]byte(nil), utf16LEBOM...), 0x00, 0xd8), want: "unpaired UTF-16 high"},
		{name: "unpaired-low", body: append(append([]byte(nil), utf16BEBOM...), 0xdc, 0x00), want: "unpaired UTF-16 low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeCLIJSONEncoding(test.body)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want error containing %q", err, test.want)
			}
		})
	}
}

func encodeTestUTF16(body []byte, littleEndian bool) []byte {
	units := utf16.Encode([]rune(string(body)))
	prefix := utf16BEBOM
	if littleEndian {
		prefix = utf16LEBOM
	}
	out := append([]byte(nil), prefix...)
	for _, unit := range units {
		if littleEndian {
			out = append(out, byte(unit), byte(unit>>8))
		} else {
			out = append(out, byte(unit>>8), byte(unit))
		}
	}
	return out
}
