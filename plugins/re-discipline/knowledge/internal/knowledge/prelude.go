package knowledge

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// A document's epistemic header - what it claims, how strongly, when it was
// last verified, and whether it has been superseded - lives only in its first
// chunk. Every other chunk of the same document reaches a caller with none of
// it, so a passage from a corrected document can be served with no indication
// that a correction exists.
//
// The prelude is that header, recomputed per chunk at index time and carried
// beside the passage. It is never concatenated into Chunk.Content: verifyChunk
// re-reads the source lines and requires an exact match, so synthesized text
// in Content would make every chunk fail verification.
const (
	preludeMaxBytes      = 384 // 96 tokens at 4 bytes per token
	preludeClaimBytes    = 200
	preludeCorrectBytes  = 120
	preludeConfidBytes   = 40
	preludeClaimStepDown = 20
)

var (
	preludeClaimRE      = regexp.MustCompile(`(?ms)^\*\*Claim:\*\*[ \t]*(.*?)(?:\n[ \t]*\n|\n\*\*|\z)`)
	preludeConfidenceRE = regexp.MustCompile(`(?m)^\*\*Confidence:\*\*[ \t]*(.*)$`)
	preludeValidityRE   = regexp.MustCompile(`(?ms)^\*\*Validity:\*\*(.*?)(?:\n\*\*|\z)`)
	preludeVerifiedRE   = regexp.MustCompile(`(?i)verified:[ \t]*(\d{4}-\d{2}-\d{2})`)
	// Status is line-anchored deliberately. An unanchored search matches
	// in-prose provenance notes such as "Supersedes the 2026-06-20 reading",
	// which scope a claim rather than retire a document. Marking a current
	// document superseded is worse than marking nothing, because it teaches
	// callers to distrust a correct claim.
	preludeSupersededByRE = regexp.MustCompile(`(?m)^\*\*Superseded-by:\*\*[ \t]*(\S+)`)
	preludeSupersedesRE   = regexp.MustCompile(`(?m)^\*\*Supersedes:\*\*[ \t]*(\S+)`)
	preludeStripRE        = regexp.MustCompile("[*`]+")
	preludeLeadRE         = regexp.MustCompile(`(?m)^[ \t]*[>#\-]+[ \t]*`)
	preludeSpaceRE        = regexp.MustCompile(`[ \t\n]+`)
)

// normalizePreludeField reduces a raw markdown fragment to a single
// whitespace-collapsed line with decoration removed.
func normalizePreludeField(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = preludeLeadRE.ReplaceAllString(value, "")
	value = preludeStripRE.ReplaceAllString(value, "")
	value = preludeSpaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

// truncateRunes cuts value to at most limit bytes without splitting a rune.
// A mid-rune cut yields invalid UTF-8, which DiscoverSources rejects outright.
func truncateRunes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut])
}

// firstSentence returns the leading sentence of a claim, bounded to limit
// bytes. A sentence ends at '.', ';' or ':' followed by whitespace.
func firstSentence(value string, limit int) string {
	for index := 0; index < len(value) && index < limit; index++ {
		switch value[index] {
		case '.', ';', ':':
			if index+1 < len(value) &&
				(value[index+1] == ' ' || value[index+1] == '\t') {
				return strings.TrimSpace(value[:index+1])
			}
		}
	}
	if len(value) <= limit {
		return value
	}
	return truncateRunes(value, limit-3) + "..."
}

// ClaimDigest digests what a document asserts: its claim, its confidence
// grade, and its supersession status. Evidence prose, formatting and
// re-verification dates are deliberately excluded, so a case pinned to this
// value survives every edit that does not change the claim itself.
func ClaimDigest(body, path string) string {
	prelude := ExtractDocumentPrelude(body, path)
	return "sha256:" + SHA256String(strings.Join([]string{
		prelude.Claim, prelude.Confidence, prelude.Status, prelude.Correction,
	}, "\x1f"))
}

// DocumentPrelude is the extracted epistemic header of a single document.
type DocumentPrelude struct {
	Title      string
	Status     string
	Correction string
	Claim      string
	Confidence string
	Verified   string
}

var (
	preludeVerdictRE      = regexp.MustCompile(`(?ms)^#{1,4}[ \t]*\d*\.?[ \t]*VERDICT\b[^\n]*\n+(.*?)(?:\n#{1,4}[ \t]|\z)`)
	preludeOverallRE      = regexp.MustCompile(`(?mi)^#{0,4}[ \t]*\**\s*OVERALL CONFIDENCE\**[: \t]*(.*)$`)
	preludeVerdictLabelRE = regexp.MustCompile(
		`^(?i)(VERDICT|DIRECT|INFERRED|CONFIRMED|REFUTED|PARTIAL)[ \t]*[:\-]+[ \t]*`)
)

// ExtractReportPrelude labels a raw run report as unnormalized provenance.
// Manager decisions live in immutable review records and normalized findings,
// never as mutable annotations inside the report itself.
func ExtractReportPrelude(body, path string) DocumentPrelude {
	prelude := DocumentPrelude{Title: normalizePreludeField(titleFromMarkdown(body, path))}
	if match := preludeVerdictRE.FindStringSubmatch(body); match != nil {
		// A verdict usually opens with its own epistemic label. Taking the
		// first sentence naively yields "DIRECT:" and discards the substance,
		// so strip a leading label before splitting. The label is not lost:
		// it is on the chunk heading and in the body.
		verdict := preludeVerdictLabelRE.ReplaceAllString(
			normalizePreludeField(match[1]), "")
		prelude.Claim = firstSentence(
			strings.TrimSpace(verdict), preludeClaimBytes)
	}
	if match := preludeOverallRE.FindStringSubmatch(body); match != nil {
		confidence := normalizePreludeField(match[1])
		if open := strings.Index(confidence, "("); open > 0 {
			confidence = strings.TrimSpace(confidence[:open])
		}
		prelude.Confidence = truncateRunes(confidence, preludeConfidBytes)
	}
	prelude.Status = "UNNORMALIZED PROVENANCE - expand a finding and review before relying on this report"
	return prelude
}

// ExtractLegacyTruthPrelude labels a migration-preserved pre-conversion truth
// document. These bytes were ratified truth before conversion and their claims
// now live in normalized findings, so the unreviewed-drafter label a raw run
// report receives would be false here. The preserved prose keeps its own
// extracted claim and confidence fields; only its status is synthesized, and
// that status names the normalized finding tier as the current authority.
func ExtractLegacyTruthPrelude(body, path string) DocumentPrelude {
	prelude := ExtractDocumentPrelude(body, path)
	prelude.Status = "SUPERSEDED BY NORMALIZED FINDING - preserved original prose; the finding is authoritative"
	return prelude
}

// ExtractDocumentPrelude reads a document body and returns its header fields.
// Every field is optional: history, backlog, active and memory documents carry
// no Claim block, and degrade to a title-only prelude of 8-15 tokens. path is
// the title fallback for a document with no heading.
func ExtractDocumentPrelude(body, path string) DocumentPrelude {
	// Every prelude pattern below anchors on LF paragraph and line breaks. A
	// CRLF checkout -- the Windows default -- would otherwise defeat the
	// blank-line terminators and let a claim run far past its paragraph,
	// disagreeing with the reviewed claim extractor, which normalizes first.
	body = normalizeDocumentLineEndings(body)
	prelude := DocumentPrelude{Title: normalizePreludeField(titleFromMarkdown(body, path))}

	if match := preludeSupersededByRE.FindStringSubmatch(body); match != nil {
		prelude.Status = "superseded"
		prelude.Correction = truncateRunes(
			normalizePreludeField(match[1]), preludeCorrectBytes)
	} else if match := preludeSupersedesRE.FindStringSubmatch(body); match != nil {
		prelude.Status = "corrected"
		prelude.Correction = truncateRunes(
			normalizePreludeField(match[1]), preludeCorrectBytes)
	}

	if match := preludeClaimRE.FindStringSubmatch(body); match != nil {
		prelude.Claim = firstSentence(
			normalizePreludeField(match[1]), preludeClaimBytes)
	}
	if match := preludeConfidenceRE.FindStringSubmatch(body); match != nil {
		confidence := normalizePreludeField(match[1])
		if open := strings.Index(confidence, "("); open > 0 {
			confidence = strings.TrimSpace(confidence[:open])
		}
		prelude.Confidence = truncateRunes(confidence, preludeConfidBytes)
	}
	if match := preludeValidityRE.FindStringSubmatch(body); match != nil {
		if verified := preludeVerifiedRE.FindStringSubmatch(match[1]); verified != nil {
			prelude.Verified = verified[1]
		}
	}
	return prelude
}

// ExtractFindingPrelude projects typed finding metadata into the bounded
// passage prelude. The normalized card is authoritative; this exists so a
// legacy chunk fallback can never shed review or validity labels.
func ExtractFindingPrelude(document SourceDocument) DocumentPrelude {
	return DocumentPrelude{
		Title:      normalizePreludeField(document.FindingID + " " + document.Title),
		Status:     normalizePreludeField(document.ReviewState + "/" + document.Validity),
		Claim:      firstSentence(document.FindingClaim, preludeClaimBytes),
		Confidence: normalizePreludeField(document.EvidenceGrade),
	}
}

// Render composes the prelude into its wire form, bounded to preludeMaxBytes.
//
// Field order is fixed and is also the inverse truncation order: verified is
// dropped first, then confidence, then the claim is shortened in steps. The
// result is deterministic and total - the same document always yields the same
// prelude, and no input can produce an over-budget or invalid-UTF-8 value.
func (prelude DocumentPrelude) Render() string {
	claim := prelude.Claim
	includeConfidence := true
	includeVerified := true
	// An unreviewed-drafter status is load-bearing rather than informational,
	// so it is exempt from truncation entirely. Roughly ten tokens.
	unreviewed := strings.HasPrefix(prelude.Status, "UNREVIEWED")

	for {
		parts := make([]string, 0, 6)
		if prelude.Title != "" {
			parts = append(parts, "title: "+prelude.Title)
		}
		// "current" is the default and is never emitted; it would cost tokens
		// on every chunk of every healthy document to say nothing.
		if prelude.Status != "" {
			parts = append(parts, "status: "+prelude.Status)
		}
		if prelude.Correction != "" {
			parts = append(parts, "correction: "+prelude.Correction)
		}
		if claim != "" {
			parts = append(parts, "claim: "+claim)
		}
		if includeConfidence && prelude.Confidence != "" {
			parts = append(parts, "confidence: "+prelude.Confidence)
		}
		if includeVerified && prelude.Verified != "" {
			parts = append(parts, "verified: "+prelude.Verified)
		}
		rendered := strings.Join(parts, " | ")
		if len(rendered) <= preludeMaxBytes {
			return rendered
		}
		switch {
		case includeVerified:
			includeVerified = false
		case includeConfidence:
			includeConfidence = false
		case len(claim) > preludeClaimStepDown:
			claim = truncateRunes(claim, len(claim)-preludeClaimStepDown)
		default:
			// Title alone still exceeds the cap: cut it and stop. An
			// unreviewed status is re-prefixed afterwards so a hard truncation
			// can never strip the one field a caller must not miss.
			cut := truncateRunes(rendered, preludeMaxBytes)
			if unreviewed && !strings.Contains(cut, "UNREVIEWED") {
				marker := "status: " + prelude.Status
				cut = marker + " | " + truncateRunes(
					cut, preludeMaxBytes-len(marker)-3)
			}
			return cut
		}
	}
}

// normalizeDocumentLineEndings converts CRLF and lone CR to LF so
// line-anchored document parsing is identical on every checkout.
func normalizeDocumentLineEndings(body string) string {
	if !strings.ContainsRune(body, '\r') {
		return body
	}
	return strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
}
