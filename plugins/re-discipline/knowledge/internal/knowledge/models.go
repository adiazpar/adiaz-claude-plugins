package knowledge

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"strings"
	"unicode"
)

const featureDimensions = 512

const (
	denseExactScanMaximum = 2000
	denseCandidateMaximum = 4096
	denseExpandedMinimum  = 100
)

func modelTokens(value string) []string {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) ||
			r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '@')
	})
	out := make([]string, 0, len(fields)*2)
	for _, field := range fields {
		field = strings.Trim(field, "-./:@")
		if len(field) < 2 || len(field) > 200 {
			continue
		}
		out = append(out, field)
	}
	base := append([]string(nil), out...)
	for i := 0; i+1 < len(base); i++ {
		out = append(out, base[i]+"\x1f"+base[i+1])
	}
	return out
}

func featureHashVector(value string) [featureDimensions]int32 {
	var vector [featureDimensions]int32
	for _, token := range modelTokens(value) {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(token))
		sum := hash.Sum64()
		index := int(sum % featureDimensions)
		sign := int32(1)
		if sum&(uint64(1)<<63) != 0 {
			sign = -1
		}
		vector[index] += sign
	}
	return vector
}

func featureDot(left, right [featureDimensions]int32) int64 {
	var score int64
	for index := 0; index < featureDimensions; index++ {
		score += int64(left[index]) * int64(right[index])
	}
	return score
}

func encodeFeatureVector(vector [featureDimensions]int32) []byte {
	body := make([]byte, featureDimensions*4)
	for index, value := range vector {
		binary.LittleEndian.PutUint32(body[index*4:index*4+4], uint32(value))
	}
	return body
}

func decodeFeatureVector(body []byte) ([featureDimensions]int32, error) {
	var vector [featureDimensions]int32
	if len(body) != featureDimensions*4 {
		return vector, errors.New("invalid persisted feature vector length")
	}
	for index := range vector {
		vector[index] = int32(binary.LittleEndian.Uint32(body[index*4 : index*4+4]))
	}
	return vector, nil
}

func vectorSignature(vector [featureDimensions]int32) uint16 {
	var signature uint16
	for bit := 0; bit < 16; bit++ {
		var projection int64
		for index, value := range vector {
			seed := uint64(index+1)*0x9e3779b97f4a7c15 ^ uint64(bit+1)*0xbf58476d1ce4e5b9
			seed ^= seed >> 30
			seed *= 0xbf58476d1ce4e5b9
			seed ^= seed >> 27
			sign := int64(1)
			if seed&(uint64(1)<<63) != 0 {
				sign = -1
			}
			projection += int64(value) * sign
		}
		if projection >= 0 {
			signature |= 1 << bit
		}
	}
	return signature
}

func signatureNeighbors(signature uint16, distance int) []uint16 {
	values := []uint16{signature}
	if distance >= 1 {
		for first := 0; first < 16; first++ {
			values = append(values, signature^(1<<first))
		}
	}
	if distance >= 2 {
		for first := 0; first < 16; first++ {
			for second := first + 1; second < 16; second++ {
				values = append(values, signature^(1<<first)^(1<<second))
			}
		}
	}
	return values
}

func linearRerank(query string, chunk Chunk) int64 {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	contentLower := strings.ToLower(chunk.Content)
	headingLower := strings.ToLower(chunk.Heading)
	pathLower := strings.ToLower(chunk.Path)
	var score int64
	if queryLower != "" {
		score += int64(strings.Count(contentLower, queryLower)) * 10000
		score += int64(strings.Count(headingLower, queryLower)) * 6000
		score += int64(strings.Count(pathLower, queryLower)) * 4000
	}
	queryTokens := SortedUnique(modelTokens(query))
	if len(queryTokens) == 0 {
		return score
	}
	contentTokens := make(map[string]bool)
	for _, token := range modelTokens(chunk.Content) {
		contentTokens[token] = true
	}
	headingTokens := make(map[string]bool)
	for _, token := range modelTokens(chunk.Heading) {
		headingTokens[token] = true
	}
	pathTokens := make(map[string]bool)
	for _, token := range modelTokens(chunk.Path) {
		pathTokens[token] = true
	}
	for _, token := range queryTokens {
		if contentTokens[token] {
			score += 1000
		}
		if headingTokens[token] {
			score += 1500
		}
		if pathTokens[token] {
			score += 1200
		}
	}
	return score
}
