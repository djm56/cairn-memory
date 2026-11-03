package ui

import "embed"

// templateFiles holds the layout and the page templates, embedded so the binary carries
// its own pages. html/template is the only template package used (DESIGN §5.5).
//
//go:embed templates/*.html
var templateFiles embed.FS

// stylesheet is the one stylesheet of the UI, embedded and served at /ui/static/app.css.
// The page's policy allows no inline style, so every rule is here.
//
//go:embed static/app.css
var stylesheet []byte
