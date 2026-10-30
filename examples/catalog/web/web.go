// Package web holds the demo page: one page for both the server and the browser build.
package web

import "embed"

// FS is the demo page and its scripts.
//
//go:embed index.html style.css app.js engine.js
var FS embed.FS
