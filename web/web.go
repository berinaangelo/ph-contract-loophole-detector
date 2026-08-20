// Package web embeds the single-page UI (build order step 8) into the
// server binary — no separate static-file directory to configure at
// deploy time, matching the project's native-process, no-Docker
// deployment choice.
package web

import "embed"

//go:embed index.html
var FS embed.FS
