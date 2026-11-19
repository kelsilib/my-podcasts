package app

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed icon.png
var appIconPNG []byte

func strokeIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

func fillIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">` + paths + `</svg>`))
}

var (
	iconPodcast      = strokeIcon(`<rect x="9" y="2" width="6" height="12" rx="3"/><path d="M19 10v1a7 7 0 0 1-14 0v-1"/><path d="M12 18v4"/><path d="M8 22h8"/>`)
	iconCompass      = strokeIcon(`<circle cx="12" cy="12" r="9.5"/><polygon points="16.2 7.8 14.1 14.1 7.8 16.2 9.9 9.9 16.2 7.8"/>`)
	iconClock        = strokeIcon(`<circle cx="12" cy="12" r="9.5"/><polyline points="12 7 12 12 15.5 14"/>`)
	iconLibrary      = strokeIcon(`<path d="M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H20v20H6.5a2.5 2.5 0 0 1 0-5H20"/>`)
	iconBookmark     = strokeIcon(`<path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/>`)
	iconBookmarkFill = fillIcon(`<path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/>`)

	iconPlay     = fillIcon(`<path d="M7 4.6v14.8c0 .8.9 1.3 1.6.9l12-7.4c.6-.4.6-1.3 0-1.7l-12-7.4C7.9 3.3 7 3.8 7 4.6z"/>`)
	iconPause    = fillIcon(`<rect x="5.5" y="4" width="4.5" height="16" rx="1.4"/><rect x="14" y="4" width="4.5" height="16" rx="1.4"/>`)
	iconNext     = fillIcon(`<path d="M4 5.6v12.8c0 .8.9 1.3 1.6.8l9.2-6.4c.6-.4.6-1.2 0-1.6L5.6 4.8C4.9 4.3 4 4.8 4 5.6z"/><rect x="17" y="4.5" width="2.8" height="15" rx="1"/>`)
	iconPrev     = fillIcon(`<g transform="matrix(-1 0 0 1 24 0)"><path d="M4 5.6v12.8c0 .8.9 1.3 1.6.8l9.2-6.4c.6-.4.6-1.2 0-1.6L5.6 4.8C4.9 4.3 4 4.8 4 5.6z"/><rect x="17" y="4.5" width="2.8" height="15" rx="1"/></g>`)
	iconSkipBack = strokeIcon(`<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/><path d="M10 10v4"/><path d="M14 10h-2v2h1.5a1 1 0 0 1 0 2H12"/>`)
	iconSkipFwd  = strokeIcon(`<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/><path d="M9 10h2a1 1 0 0 1 0 2h-1"/><path d="M10 12h1a1 1 0 0 1 0 2H9"/><circle cx="15" cy="12" r="2"/>`)

	iconVolume  = strokeIcon(`<path d="M11 4.7a.7.7 0 0 0-1.2-.5L6.4 7.6A1.4 1.4 0 0 1 5.4 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.4a1.4 1.4 0 0 1 1 .4l3.4 3.4a.7.7 0 0 0 1.2-.5z"/><path d="M15.5 8.5a5 5 0 0 1 0 7"/><path d="M19 5.5a9 9 0 0 1 0 13"/>`)
	iconMute    = strokeIcon(`<path d="M11 4.7a.7.7 0 0 0-1.2-.5L6.4 7.6A1.4 1.4 0 0 1 5.4 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.4a1.4 1.4 0 0 1 1 .4l3.4 3.4a.7.7 0 0 0 1.2-.5z"/><line x1="22" x2="16" y1="9" y2="15"/><line x1="16" x2="22" y1="9" y2="15"/>`)
	iconBack    = strokeIcon(`<path d="m15 18-6-6 6-6"/>`)
	iconForward = strokeIcon(`<path d="m9 18 6-6-6-6"/>`)
	iconUser    = strokeIcon(`<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>`)
	iconRefresh = strokeIcon(`<path d="M21 12a9 9 0 1 1-2.64-6.36L21 8"/><path d="M21 3v5h-5"/>`)
	iconSignOut = strokeIcon(`<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>`)
	iconPlus    = strokeIcon(`<path d="M12 5v14"/><path d="M5 12h14"/>`)
	iconCheck   = strokeIcon(`<path d="m5 12 4 4L19 6"/>`)
	iconInfo  = strokeIcon(`<circle cx="12" cy="12" r="9.5"/><path d="M12 16v-4"/><circle cx="12" cy="8" r=".5" fill="currentColor"/>`)
	iconClose = strokeIcon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	iconMore  = fillIcon(`<circle cx="5.5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="18.5" cy="12" r="1.7"/>`)
	iconQuote = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M20.5 11.5c0 4.1-3.8 7.5-8.5 7.5-1.3 0-2.6-.3-3.7-.7L3.5 19.6l1.3-3.7C4 14.7 3.5 13.2 3.5 11.5 3.5 7.4 7.3 4 12 4s8.5 3.4 8.5 7.5z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/><path d="M8.6 9.5h2.5v2.4c0 1.4-.9 2.3-2.2 2.5V13.2c.6-.1 1-.6 1-1.2H8.6V9.5zm4.5 0h2.5v2.4c0 1.4-.9 2.3-2.2 2.5V13.2c.6-.1 1-.6 1-1.2h-1.3V9.5z" fill="currentColor"/></svg>`))
)
