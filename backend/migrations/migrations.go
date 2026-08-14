// Package migrations embeds the numbered SQL migration files so the binary can
// apply them without shipping the source tree.
//
// Naming: NNN_<owner>_<subject>.sql, where owner is `b` or `c` (roadmap §5).
// Person B owns 002–008 and 012; Person C owns 009–011.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
