package knowledge

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"unicode/utf8"
)

const sentenceVectorScale int64 = 32767

type bundledEmbedding struct {
	identity   ModelIdentity
	dimensions int
	scale      int
	offsets    map[string]int
	vectors    []int8
}

var bundledEmbeddings = struct {
	sync.RWMutex
	models map[string]*bundledEmbedding
}{
	models: map[string]*bundledEmbedding{},
}

func embeddingRegistryKey(id, specSHA256, artifactSHA256 string) string {
	return id + "@" + specSHA256 + "@" + artifactSHA256
}

func registerBundledEmbedding(model ModelSpec, body []byte) error {
	const headerSize = 20
	if len(body) < headerSize || !bytes.Equal(body[:8], []byte{'R', 'D', 'G', 'L', 'V', 'Q', '8', 0}) {
		return errors.New("invalid GloVe Q8 artifact magic")
	}
	version := int(binary.LittleEndian.Uint16(body[8:10]))
	dimensions := int(binary.LittleEndian.Uint16(body[10:12]))
	count := int(binary.LittleEndian.Uint32(body[12:16]))
	scale := int(binary.LittleEndian.Uint16(body[16:18]))
	reserved := binary.LittleEndian.Uint16(body[18:20])
	if version != 1 || dimensions != model.Dimensions || dimensions != 50 ||
		count != 50000 || scale != 127 || reserved != 0 {
		return fmt.Errorf(
			"unsupported GloVe Q8 header version=%d dimensions=%d count=%d scale=%d",
			version, dimensions, count, scale)
	}
	offsets := make(map[string]int, count)
	vectors := make([]int8, 0, count*dimensions)
	cursor := headerSize
	previous := ""
	for index := 0; index < count; index++ {
		if cursor+2 > len(body) {
			return errors.New("truncated GloVe Q8 token length")
		}
		length := int(binary.LittleEndian.Uint16(body[cursor : cursor+2]))
		cursor += 2
		if length < 1 || length > 200 || cursor+length+dimensions > len(body) {
			return errors.New("invalid GloVe Q8 entry bounds")
		}
		tokenBytes := body[cursor : cursor+length]
		cursor += length
		if !utf8.Valid(tokenBytes) {
			return errors.New("GloVe Q8 token is not valid UTF-8")
		}
		token := string(tokenBytes)
		if index > 0 && token <= previous {
			return errors.New("GloVe Q8 tokens are not strictly byte-sorted")
		}
		previous = token
		offsets[token] = len(vectors)
		for _, value := range body[cursor : cursor+dimensions] {
			vectors = append(vectors, int8(value))
		}
		cursor += dimensions
	}
	if cursor != len(body) {
		return errors.New("GloVe Q8 artifact has trailing bytes")
	}
	loaded := &bundledEmbedding{
		identity:   modelIdentity(model, "fixed-int64-v1"),
		dimensions: dimensions, scale: scale, offsets: offsets, vectors: vectors,
	}
	if err := validateBundledEmbeddingCanary(model, loaded); err != nil {
		return err
	}
	key := embeddingRegistryKey(model.ID, model.SpecSHA256, model.ArtifactSHA256)
	bundledEmbeddings.Lock()
	bundledEmbeddings.models[key] = loaded
	bundledEmbeddings.Unlock()
	return nil
}

func validateBundledEmbeddingCanary(
	model ModelSpec,
	embedding *bundledEmbedding,
) error {
	if model.ID != "builtin:glove-6b-50d-top50k-q8-v1" {
		return nil
	}
	query := embedding.sentenceVector("physician treats disease hospital")
	target := embedding.sentenceVector("doctor cures illness clinic")
	decoy := embedding.sentenceVector("stock dividend currency market")
	targetScore, err := vectorDot(query, target)
	if err != nil {
		return err
	}
	decoyScore, err := vectorDot(query, decoy)
	if err != nil {
		return err
	}
	scaleSquared := sentenceVectorScale * sentenceVectorScale
	if targetScore < scaleSquared*70/100 ||
		decoyScore > scaleSquared*30/100 ||
		targetScore-decoyScore < scaleSquared*40/100 {
		return fmt.Errorf(
			"semantic canary failed target=%d decoy=%d scale=%d",
			targetScore, decoyScore, scaleSquared,
		)
	}
	return nil
}

// resolveEmbeddingModel keeps package-internal diagnostic tests compatible with
// the historical model-ID form. Production retrieval always supplies the exact
// service-selected identity. An ambiguous ID is rejected rather than borrowing
// another asset root's model.
func resolveEmbeddingModel(value any) (ModelIdentity, error) {
	if identity, ok := value.(ModelIdentity); ok {
		return identity, nil
	}
	id, ok := value.(string)
	if !ok || id == "" {
		return ModelIdentity{}, errors.New("embedding model identity is required")
	}
	if id == "builtin:feature-hash-512-v1" {
		return ModelIdentity{
			ID: id, Implementation: "builtin", Dimensions: featureDimensions,
			NumericalBackend: "fixed-int64-v1",
		}, nil
	}
	bundledEmbeddings.RLock()
	defer bundledEmbeddings.RUnlock()
	var result ModelIdentity
	for _, embedding := range bundledEmbeddings.models {
		if embedding.identity.ID != id {
			continue
		}
		if result.ID != "" && result != embedding.identity {
			return ModelIdentity{}, fmt.Errorf(
				"embedding model ID %q is ambiguous without an exact identity", id)
		}
		result = embedding.identity
	}
	if result.ID == "" {
		return ModelIdentity{}, fmt.Errorf("embedding model %q is unavailable", id)
	}
	return result, nil
}

func embeddingVector(model ModelIdentity, value string) ([]int32, error) {
	switch model.Implementation {
	case "builtin":
		vector := featureHashVector(value)
		result := make([]int32, len(vector))
		copy(result, vector[:])
		return result, nil
	case "bundled-local":
		key := embeddingRegistryKey(model.ID, model.SpecSHA256, model.ArtifactSHA256)
		bundledEmbeddings.RLock()
		embedding := bundledEmbeddings.models[key]
		bundledEmbeddings.RUnlock()
		if embedding == nil {
			return nil, fmt.Errorf("bundled embedding %q is not loaded", model.ID)
		}
		return embedding.sentenceVector(value), nil
	default:
		return nil, fmt.Errorf("embedding implementation %q is unavailable", model.Implementation)
	}
}

func (embedding *bundledEmbedding) sentenceVector(value string) []int32 {
	sum := make([]int64, embedding.dimensions)
	tokens := modelTokens(value)
	for _, token := range tokens {
		if strings.ContainsRune(token, '\x1f') {
			continue
		}
		if offset, ok := embedding.offsets[token]; ok {
			for dimension := 0; dimension < embedding.dimensions; dimension++ {
				sum[dimension] += int64(embedding.vectors[offset+dimension])
			}
			continue
		}
		// Deterministic low-amplitude OOV features retain useful identifier
		// overlap without masquerading as learned semantics.
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(token))
		value := hash.Sum64()
		dimension := int(value % uint64(embedding.dimensions))
		sign := int64(1)
		if value&(uint64(1)<<63) != 0 {
			sign = -1
		}
		sum[dimension] += sign * int64(maxInt(1, embedding.scale/8))
	}
	var normSquared uint64
	for _, component := range sum {
		normSquared += uint64(component * component)
	}
	result := make([]int32, embedding.dimensions)
	if normSquared == 0 {
		return result
	}
	norm := integerSquareRoot(normSquared)
	if norm == 0 {
		return result
	}
	for index, component := range sum {
		result[index] = int32(component * sentenceVectorScale / int64(norm))
	}
	return result
}

func integerSquareRoot(value uint64) uint64 {
	if value == 0 {
		return 0
	}
	x := value
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + value/x) / 2
	}
	return x
}

func encodeVector(vector []int32) []byte {
	body := make([]byte, len(vector)*4)
	for index, value := range vector {
		binary.LittleEndian.PutUint32(body[index*4:index*4+4], uint32(value))
	}
	return body
}

func decodeVector(body []byte, dimensions int) ([]int32, error) {
	if dimensions < 1 || dimensions > 4096 || len(body) != dimensions*4 {
		return nil, errors.New("invalid persisted embedding vector length")
	}
	vector := make([]int32, dimensions)
	for index := range vector {
		vector[index] = int32(binary.LittleEndian.Uint32(body[index*4 : index*4+4]))
	}
	return vector, nil
}

func vectorDot(left, right []int32) (int64, error) {
	if len(left) != len(right) {
		return 0, errors.New("embedding vector dimensions differ")
	}
	var score int64
	for index := range left {
		score += int64(left[index]) * int64(right[index])
	}
	return score, nil
}

func vectorSignatureSlice(vector []int32) uint16 {
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
