// Package gradingdata embeds the default grading rules.
package gradingdata

import "embed"

//go:embed *.json
var FS embed.FS
