# Third-Party Notices

The re-discipline knowledge runtime is first-party source distributed under
the MIT License in the re-discipline plugin's `LICENSE` file; the controlling
first-party terms are also reproduced below so every generated binary package
remains self-contained. Its packaged binaries additionally include the
following third-party Go modules, Go standard-library code, and data under
their own terms.

| Component | Version | License |
|---|---:|---|
| Go standard library | go1.26.5 | BSD-3-Clause |
| `modernc.org/sqlite` | v1.54.0 | BSD-3-Clause |
| `modernc.org/libc` | v1.74.1 | BSD-3-Clause plus notices below |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause |
| `modernc.org/memory` | v1.11.0 | BSD-3-Clause plus notices below |
| `github.com/dustin/go-humanize` | v1.0.1 | MIT |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/mattn/go-isatty` | v0.0.20 | MIT |
| `github.com/ncruces/go-strftime` | v1.0.0 | MIT |
| `github.com/remyoudompheng/bigfft` | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause |
| `golang.org/x/sys` | v0.47.0 | BSD-3-Clause |
| Stanford GloVe 6B 50-dimensional data subset | artifact v1 | Open Data Commons PDDL 1.0 |

The module versions above are fixed by `go.mod` and `go.sum`. They are the
third-party modules actually linked into the release runtime according to its
embedded Go build information; modules used only while generating or compiling
code are not redistributed in the runtime binary. The packager rejects a
binary whose exact Go toolchain or linked module/version set is not covered by
this table. This file is copied into every generated `knowledge/bin/` package
and covered by its checksum manifest.

## First-party MIT License

MIT License

Copyright (c) 2026 Alex Diaz

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Packaged GloVe data

The shared `models/artifacts/glove-6b-50d-top50k-q8-v1.bin` artifact is a
deterministically quantized subset of the Stanford GloVe 6B 50-dimensional
pretrained vector data. Its source archive, member, checksum, conversion
algorithm, binary format, artifact checksum, and reproduction command are
recorded in `models/artifacts/README.md`.

The upstream data is made available under the Open Data Commons Public Domain
Dedication and License 1.0:

<https://opendatacommons.org/licenses/pddl/1-0/>

PDDL 1.0 dedicates covered copyright and database rights to the public domain,
waives those rights where possible, and supplies a worldwide, royalty-free
fallback license where dedication or waiver is unavailable. The work is
provided as-is without warranty. The complete controlling text is at the link
above.

## BSD-3-Clause components

Copyright (c) 2009 The Go Authors. All rights reserved.

Copyright (c) 2017 The Sqlite Authors. All rights reserved.

Copyright (c) 2017 The Libc Authors. All rights reserved.

Copyright (c) 2014 The mathutil Authors. All rights reserved.

Copyright (c) 2017 The Memory Authors. All rights reserved.

Copyright (c) 2012 The Go Authors. All rights reserved.

Copyright 2009 The Go Authors.

Copyright (c) 2009,2014 Google Inc. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
   this list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.
3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
POSSIBILITY OF SUCH DAMAGE.

The Go-derived components use the same conditions with clause 3 naming Google
Inc. rather than the generic copyright holder:

> Neither the name of Google Inc. nor the names of its contributors may be
> used to endorse or promote products derived from this software without
> specific prior written permission.

`modernc.org/memory` also contains mmap-go:

Copyright (c) 2011, Evan Shaw <edsrzf@gmail.com>. All rights reserved.

It is distributed under the BSD-3-Clause terms above.

## MIT components

Copyright (c) 2005-2008 Dustin Sallings <dustin@spy.net>

Copyright (c) Yasuhiro MATSUMOTO <mattn.jp@gmail.com>

Copyright (c) 2022 Nuno Cruces

Copyright (c) 2005-2020 Rich Felker, et al.

Copyright (c) 2012 Dominik Honnef

Copyright (c) 2003-2025 Eelco Dolstra and the Nixpkgs/NixOS contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## modernc.org/libc incorporated notices

`modernc.org/libc` includes or derives code from the following sources. These
notices are reproduced from its `LICENSE-3RD-PARTY.md`.

### Go

Source: <https://github.com/golang/go>

Copyright (c) 2009 The Go Authors. All rights reserved. The BSD-3-Clause terms
and Google-specific third clause above apply.

### musl libc

Source: <https://musl.libc.org/>

musl as a whole is licensed under the MIT terms above.

Authors and contributors include:

A. Wilcox; Ada Worcester; Alex Dowad; Alex Suykov; Alexander Monakov; Andre
McCurdy; Andrew Kelley; Anthony G. Basile; Aric Belsito; Arvid Picciani;
Bartosz Brachaczek; Benjamin Peterson; Bobby Bingham; Boris Brezillon; Brent
Cook; Chris Spiegel; Clement Vasseur; Daniel Micay; Daniel Sabogal;
Daurnimator; David Carlier; David Edelsohn; Denys Vlasenko; Dmitry Ivanov;
Dmitry V. Levin; Drew DeVault; Emil Renner Berthing; Fangrui Song; Felix
Fietkau; Felix Janda; Gianluca Anzolin; Hauke Mehrtens; He X; Hiltjo
Posthuma; Isaac Dunham; Jaydeep Patil; Jens Gustedt; Jeremy Huntwork;
Jo-Philipp Wich; Joakim Sindholt; John Spencer; Julien Ramseier; Justin
Cormack; Kaarle Ritvanen; Khem Raj; Kylie McClain; Leah Neukirchen; Luca
Barbato; Luka Perkov; M Farkas-Dyck (Strake); Mahesh Bodapati; Markus
Wichmann; Masanori Ogino; Michael Clark; Michael Forney; Mikhail Kremnyov;
Natanael Copa; Nicholas J. Kain; orc; Pascal Cuoq; Patrick Oppenlander; Petr
Hosek; Petr Skocik; Pierre Carrier; Reini Urban; Rich Felker; Richard
Pennington; Ryan Fairfax; Samuel Holland; Segev Finer; Shiz; sin; Solar
Designer; Stefan Kristiansson; Stefan O'Rear; Szabolcs Nagy; Timo Teras;
Trutz Behn; Valentin Ochs; Will Dietz; William Haddon; and William Pitcock.

Portions of musl are derived from third-party works under terms compatible
with the MIT license:

- The TRE regular-expression implementation is Copyright (c) 2001-2008 Ville
  Laurikari and is licensed under a 2-clause BSD license. The included version
  was heavily modified by Rich Felker in 2012.
- Math and complex-library code includes work Copyright (c) 1993, 2004 Sun
  Microsystems; Copyright (c) 2003-2011 David Schultz; Copyright (c)
  2003-2009 Steven G. Kargl; Copyright (c) 2003-2009 Bruce D. Evans;
  Copyright (c) 2008 Stephen L. Moshier; and Copyright (c) 2017-2018 Arm
  Limited, under permissive terms identified in the source files.
- ARM memcpy code is Copyright (c) 2008 The Android Open Source Project under
  a 2-clause BSD license.
- AArch64 memcpy and memset code is Copyright (c) 1999-2019 Arm Limited.
- DES crypt code is Copyright (c) 1994 David Burren under a BSD license.
- Blowfish crypt code was written by Solar Designer and placed into the public
  domain, with a fallback permissive license for jurisdictions that do not
  recognize the public domain.
- Smoothsort code is Copyright (c) 2011 Valentin Ochs under an MIT-style
  license.
- The x86_64 port by Nicholas J. Kain, the mips and microblaze ports by
  Richard Pennington, the mips64 port by Imagination Technologies, and the
  powerpc port by Richard Pennington and John Spencer use standard MIT terms.

Permission was granted by Bobby Bingham, John Spencer, Nicholas J. Kain, Rich
Felker, Richard Pennington, Stefan Kristiansson, and Szabolcs Nagy for public
headers and CRT files intended to be linked into applications to omit the
otherwise-required copyright and permission notice.

### go-netdb

Source: <https://github.com/dominikh/go-netdb>

Copyright (c) 2012 Dominik Honnef. The MIT terms above apply.

### NixOS/nixpkgs

Source: <https://github.com/NixOS/nixpkgs>

Copyright (c) 2003-2025 Eelco Dolstra and the Nixpkgs/NixOS contributors. The
MIT terms above apply.
