// Package skill carries the Outline agent skill definition that
// `outline skill install` writes into a harness's skills directory.
package skill

import _ "embed"

// Outline is the Outline SKILL.md exactly as shipped with the CLI.
//
//go:embed outline/SKILL.md
var Outline []byte
