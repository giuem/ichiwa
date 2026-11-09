package app

import (
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

func filled(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">` + body + `</svg>`))
}

const volumeBody = `<polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" fill="currentColor"/>`

// symbolFallbacks draws the SF Symbols of the app on the systems without
// them, with Lucide icons (ISC license) adjusted to match.
var symbolFallbacks = map[string]*ui.SVG{
	"chevron.left":                       iconChevronLeft,
	"sidebar.right":                      lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/>`),
	"play.fill":                          iconPlayFill,
	"pause.fill":                         filled(`<rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/>`),
	"backward.end.fill":                  lucide(`<polygon points="19 20 9 12 19 4 19 20" fill="currentColor"/><line x1="5" x2="5" y1="19" y2="5"/>`),
	"forward.end.fill":                   lucide(`<polygon points="5 4 15 12 5 20 5 4" fill="currentColor"/><line x1="19" x2="19" y1="5" y2="19"/>`),
	"pin.fill":                           lucide(`<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" fill="currentColor"/>`),
	"arrow.up.left.and.arrow.down.right": lucide(`<polyline points="9 3 3 3 3 9"/><polyline points="15 21 21 21 21 15"/><line x1="3" x2="10" y1="3" y2="10"/><line x1="21" x2="14" y1="21" y2="14"/>`),
	"arrow.down.right.and.arrow.up.left": lucide(`<polyline points="10 4 10 10 4 10"/><polyline points="14 20 14 14 20 14"/><line x1="3" x2="10" y1="3" y2="10"/><line x1="21" x2="14" y1="21" y2="14"/>`),
	"speaker.slash.fill":                 lucide(volumeBody + `<line x1="22" x2="16" y1="9" y2="15"/><line x1="16" x2="22" y1="9" y2="15"/>`),
	"speaker.wave.1.fill":                lucide(volumeBody + `<path d="M15.54 8.46a5 5 0 0 1 0 7.07"/>`),
	"speaker.wave.2.fill":                lucide(volumeBody + `<path d="M15.54 8.46a5 5 0 0 1 0 7.07"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/>`),
	"exclamationmark.triangle":           lucide(`<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>`),
	"waveform":                           lucide(`<path d="M2 10v3"/><path d="M6 6v11"/><path d="M10 3v18"/><path d="M14 8v7"/><path d="M18 5v13"/><path d="M22 10v3"/>`),
	"lock.fill":                          lucide(`<rect width="18" height="11" x="3" y="11" rx="2" fill="currentColor"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`),
}

// sfIcon is the SF Symbol called name, or its fallback outside macOS.
func sfIcon(c *ui.Context, name string) sfsymbols.SymbolElement {
	return sfsymbols.Symbol(c, name).Fallback(symbolFallbacks[name])
}
