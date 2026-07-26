#!/usr/bin/env python3
"""Build the deterministic re-discipline GloVe Q8 embedding artifact."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from decimal import Decimal, ROUND_HALF_UP, localcontext
from pathlib import Path
import struct
import tempfile
import zipfile


SOURCE_URL = "https://nlp.stanford.edu/data/glove.6B.zip"
SOURCE_SHA256 = "617afb2fe6cbd085c235baf7a465b96f4112bd7f7ccb2b2cbd649fed9cbcf2fb"
SOURCE_MEMBER = "glove.6B.50d.txt"
ARTIFACT_NAME = "glove-6b-50d-top50k-q8-v1.bin"
MAGIC = b"RDGLVQ8\x00"
FORMAT_VERSION = 1
DIMENSIONS = 50
VOCABULARY_SIZE = 50_000
QUANTIZATION_SCALE = 127
HEADER = struct.Struct("<8sHHIHH")
TOKEN_LENGTH = struct.Struct("<H")
VECTOR = struct.Struct(f"<{DIMENSIONS}b")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def quantize(values: list[Decimal]) -> tuple[int, ...]:
    with localcontext() as context:
        context.prec = 50
        norm_squared = sum((value * value for value in values), Decimal(0))
        if norm_squared == 0:
            raise ValueError("zero-length upstream vector")
        norm = norm_squared.sqrt()
        output = []
        for value in values:
            scaled = (value * QUANTIZATION_SCALE / norm).to_integral_value(
                rounding=ROUND_HALF_UP
            )
            output.append(max(-QUANTIZATION_SCALE, min(QUANTIZATION_SCALE, int(scaled))))
        return tuple(output)


def read_vocabulary(source_zip: Path) -> list[tuple[bytes, tuple[int, ...]]]:
    entries: list[tuple[bytes, tuple[int, ...]]] = []
    seen: set[bytes] = set()
    with zipfile.ZipFile(source_zip, "r") as archive:
        names = set(archive.namelist())
        if SOURCE_MEMBER not in names:
            raise ValueError(f"{SOURCE_MEMBER!r} is missing from the source archive")
        with archive.open(SOURCE_MEMBER, "r") as source:
            for line_number, raw_line in enumerate(source, start=1):
                fields = raw_line.rstrip(b"\r\n").split()
                if len(fields) != DIMENSIONS + 1:
                    raise ValueError(
                        f"{SOURCE_MEMBER}:{line_number}: expected "
                        f"{DIMENSIONS + 1} fields, found {len(fields)}"
                    )
                token = fields[0]
                if not token or len(token) > 0xFFFF:
                    raise ValueError(
                        f"{SOURCE_MEMBER}:{line_number}: invalid token length"
                    )
                token.decode("utf-8", errors="strict")
                if token in seen:
                    continue
                try:
                    values = [Decimal(field.decode("ascii")) for field in fields[1:]]
                except Exception as error:
                    raise ValueError(
                        f"{SOURCE_MEMBER}:{line_number}: invalid numeric vector"
                    ) from error
                entries.append((token, quantize(values)))
                seen.add(token)
                if len(entries) == VOCABULARY_SIZE:
                    break
    if len(entries) != VOCABULARY_SIZE:
        raise ValueError(
            f"expected {VOCABULARY_SIZE} unique vectors, found {len(entries)}"
        )
    entries.sort(key=lambda entry: entry[0])
    return entries


def write_artifact(
    output: Path, entries: list[tuple[bytes, tuple[int, ...]]]
) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{output.name}.", suffix=".tmp", dir=output.parent
    )
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(
                HEADER.pack(
                    MAGIC,
                    FORMAT_VERSION,
                    DIMENSIONS,
                    len(entries),
                    QUANTIZATION_SCALE,
                    0,
                )
            )
            for token, vector in entries:
                handle.write(TOKEN_LENGTH.pack(len(token)))
                handle.write(token)
                handle.write(VECTOR.pack(*vector))
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, output)
    finally:
        try:
            temporary.unlink()
        except FileNotFoundError:
            pass


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Verify the pinned Stanford GloVe 6B archive and build the "
            "re-discipline top-50,000 Q8 artifact."
        )
    )
    parser.add_argument("source_zip", type=Path, help=f"downloaded {SOURCE_URL}")
    parser.add_argument(
        "output",
        nargs="?",
        type=Path,
        default=Path(__file__).resolve().parents[1] / "models" / "artifacts" / ARTIFACT_NAME,
    )
    arguments = parser.parse_args()

    actual_source_sha256 = sha256_file(arguments.source_zip)
    if actual_source_sha256 != SOURCE_SHA256:
        raise SystemExit(
            "source archive checksum mismatch: "
            f"expected {SOURCE_SHA256}, found {actual_source_sha256}"
        )

    entries = read_vocabulary(arguments.source_zip)
    write_artifact(arguments.output, entries)
    result = {
        "artifact": str(arguments.output.resolve()),
        "artifactSha256": sha256_file(arguments.output),
        "dimensions": DIMENSIONS,
        "formatVersion": FORMAT_VERSION,
        "quantization": "per-vector-l2-normalized-signed-int8",
        "quantizationScale": QUANTIZATION_SCALE,
        "sourceMember": SOURCE_MEMBER,
        "sourceSha256": actual_source_sha256,
        "vocabularySize": len(entries),
    }
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
