package knowledge

import _ "embed"

// PapersJSON is the curated offline catalog used by the deterministic runtime.
//
//go:embed papers.json
var PapersJSON []byte
