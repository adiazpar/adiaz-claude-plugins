package main

import (
	"bytes"
	"errors"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	utf8BOM    = []byte{0xef, 0xbb, 0xbf}
	utf16LEBOM = []byte{0xff, 0xfe}
	utf16BEBOM = []byte{0xfe, 0xff}
	utf32LEBOM = []byte{0xff, 0xfe, 0x00, 0x00}
	utf32BEBOM = []byte{0x00, 0x00, 0xfe, 0xff}
)

// normalizeCLIJSONEncoding accepts the encodings emitted by supported host
// shells without weakening the JSON decoder. PowerShell 5.1 commonly writes
// redirected files as BOM-marked UTF-16LE; newer hosts normally use UTF-8.
// UTF-16 without a BOM is intentionally ambiguous and remains invalid JSON.
func normalizeCLIJSONEncoding(body []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(body, utf32LEBOM) || bytes.HasPrefix(body, utf32BEBOM):
		return nil, errors.New("JSON input uses unsupported UTF-32 encoding; use UTF-8 or BOM-marked UTF-16")
	case bytes.HasPrefix(body, utf8BOM):
		body = body[len(utf8BOM):]
	case bytes.HasPrefix(body, utf16LEBOM):
		return decodeCLIUTF16(body[len(utf16LEBOM):], true)
	case bytes.HasPrefix(body, utf16BEBOM):
		return decodeCLIUTF16(body[len(utf16BEBOM):], false)
	}
	if !utf8.Valid(body) {
		return nil, errors.New("JSON input is not valid UTF-8; PowerShell UTF-16 input must include its byte-order mark")
	}
	return body, nil
}

func decodeCLIUTF16(body []byte, littleEndian bool) ([]byte, error) {
	if len(body)%2 != 0 {
		return nil, errors.New("JSON input contains a truncated UTF-16 code unit")
	}
	units := make([]uint16, len(body)/2)
	for index := range units {
		left, right := body[index*2], body[index*2+1]
		if littleEndian {
			units[index] = uint16(left) | uint16(right)<<8
		} else {
			units[index] = uint16(left)<<8 | uint16(right)
		}
	}
	out := make([]byte, 0, len(body))
	for index := 0; index < len(units); index++ {
		unit := units[index]
		var value rune
		switch {
		case 0xd800 <= unit && unit <= 0xdbff:
			if index+1 >= len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
				return nil, errors.New("JSON input contains an unpaired UTF-16 high surrogate")
			}
			value = utf16.DecodeRune(rune(unit), rune(units[index+1]))
			index++
		case 0xdc00 <= unit && unit <= 0xdfff:
			return nil, errors.New("JSON input contains an unpaired UTF-16 low surrogate")
		default:
			value = rune(unit)
		}
		out = utf8.AppendRune(out, value)
	}
	return out, nil
}
