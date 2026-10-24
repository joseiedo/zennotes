package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:embed all:dist-public
var distPublic embed.FS

// Dist returns the embedded PWA bundle rooted at `dist/`. When the
// client bundle has not been built yet, the subtree is empty and the
// caller should fall back to proxying to Vite dev in development.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

// PublicDist returns the embedded public-site bundle rooted at `dist-public/`
// (public.html + assets), served at /_zn/static/.
func PublicDist() (fs.FS, error) {
	return fs.Sub(distPublic, "dist-public")
}
