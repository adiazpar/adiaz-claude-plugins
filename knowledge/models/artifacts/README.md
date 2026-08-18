# Packaged Embedding Artifact

`glove-6b-50d-top50k-q8-v1.bin` is a deterministic, mechanically
quantized subset of Stanford GloVe 6B 50-dimensional pretrained vectors. It
is shared by every packaged runtime target; it is not copied into initialized
projects and it requires no network access at runtime.

## Provenance

| Field | Value |
|---|---|
| Upstream project | Stanford GloVe |
| Upstream page | <https://nlp.stanford.edu/projects/glove/> |
| Source archive | <https://nlp.stanford.edu/data/glove.6B.zip> |
| Source archive SHA-256 | `617afb2fe6cbd085c235baf7a465b96f4112bd7f7ccb2b2cbd649fed9cbcf2fb` |
| Source member | `glove.6B.50d.txt` |
| Upstream license | PDDL 1.0 |
| License text | <https://opendatacommons.org/licenses/pddl/1-0/> |
| Packaged artifact SHA-256 | `fb108eef095f00bcc06a38e10d7f9671d9e6664ab79ae8a2c1cef5b31375b2ab` |
| Packaged artifact size | 2,949,669 bytes |

The artifact contains the first 50,000 distinct tokens in the upstream
frequency ordering. Each upstream vector is L2-normalized with decimal
arithmetic, scaled by 127, and rounded half away from zero into signed
8-bit elements. Entries are then sorted by their UTF-8 token bytes.
This is conversion and quantization of pretrained data, not project-specific training.
It does not fine-tune the vectors for an initialized project.

## Binary Format

All integers are little-endian.

| Field | Encoding |
|---|---|
| Magic | 8 bytes: `RDGLVQ8` followed by NUL |
| Format version | unsigned 16-bit integer, value `1` |
| Dimensions | unsigned 16-bit integer, value `50` |
| Vocabulary count | unsigned 32-bit integer, value `50000` |
| Quantization scale | unsigned 16-bit integer, value `127` |
| Reserved | unsigned 16-bit integer, value `0` |
| Entry token length | unsigned 16-bit integer |
| Entry token | UTF-8 bytes |
| Entry vector | 50 signed 8-bit integers |

The token-length, token, and vector fields repeat exactly 50,000 times. No
trailing bytes are permitted.

## Reproduction

Download the pinned archive without extracting it, then run:

```text
python knowledge/scripts/build_glove_artifact.py path/to/glove.6B.zip
```

The builder checks the complete source archive before reading its single
required member and atomically replaces the output. Two clean runs must
produce the packaged SHA-256 above.
