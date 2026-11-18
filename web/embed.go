// Package web contains the self-contained browser application.
package web

import "embed"

// Assets is served by the application server, with index.html as the SPA fallback.
//go:embed *.html *.css *.js
var Assets embed.FS
