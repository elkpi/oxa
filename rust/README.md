# oxa Rust implementation (roadmap placeholder)

The Rust implementation is planned **after v1**. It is the first language to
follow the Go reference implementation, per the roadmap order
Rust → Python → C++.

Once started, this implementation must conform to the **same shared
`vectors/` golden set** as every other oxa implementation — Rust gets no
vector set of its own, and CI runs the identical vectors against it.

## Vectors location convention

Test code must **not** hard-code a path to the vectors. Instead, starting
from this implementation directory, **walk up parent directories** until you
find a directory containing both `vectors/` and `.git/` — that is the
repository root, and `vectors/` beneath it is the golden set. **Skip the
vector tests** (with a clear message) if no such root is found, so the crate
can still build and test outside the monorepo.
