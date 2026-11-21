// Draws the app icon: swift scripts/icon.swift resources/icon.png
// then: sips -z 128 128 resources/icon.png --out internal/app/assets/logo.png
//
// Episodes stacked like cards, portrait as short dramas are shot, the
// front one playing: one episode, then the next.
import AppKit

let S: CGFloat = 1024
let cs = CGColorSpace(name: CGColorSpace.displayP3)!
let ctx = CGContext(data: nil, width: Int(S), height: Int(S), bitsPerComponent: 8, bytesPerRow: 0, space: cs, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
func c(_ hex: UInt32, _ a: CGFloat = 1) -> CGColor {
  CGColor(colorSpace: cs, components: [CGFloat((hex>>16)&255)/255, CGFloat((hex>>8)&255)/255, CGFloat(hex&255)/255, a])!
}
func grad(_ cols: [CGColor], _ locs: [CGFloat]) -> CGGradient { CGGradient(colorsSpace: cs, colors: cols as CFArray, locations: locs)! }
func rounded(_ r: CGRect, _ radius: CGFloat) -> CGPath { CGPath(roundedRect: r, cornerWidth: radius, cornerHeight: radius, transform: nil) }
func shadowFill(_ path: CGPath, _ color: CGColor, shadow: CGColor, y: CGFloat, blur: CGFloat) {
  ctx.saveGState()
  ctx.setShadow(offset: CGSize(width: 0, height: y), blur: blur, color: shadow)
  ctx.addPath(path); ctx.setFillColor(color); ctx.fillPath()
  ctx.restoreGState()
}

// Body: the macOS icon grid, 824 points with rounded corners, warm orange
// at the top left to pink at the bottom right.
let body = CGRect(x: 100, y: 100, width: 824, height: 824)
let bodyPath = rounded(body, 186)
shadowFill(bodyPath, c(0xFF6A4E), shadow: c(0x000000, 0.28), y: -14, blur: 28)
ctx.saveGState()
ctx.addPath(bodyPath); ctx.clip()
ctx.drawLinearGradient(grad([c(0xFFB13D), c(0xFF6A4E), c(0xF2366F)], [0, 0.5, 1]), start: CGPoint(x: 180, y: 924), end: CGPoint(x: 844, y: 100), options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
// A soft light from the top left.
ctx.drawRadialGradient(grad([c(0xFFFFFF, 0.26), c(0xFFFFFF, 0)], [0, 1]), startCenter: CGPoint(x: 300, y: 840), startRadius: 0, endCenter: CGPoint(x: 300, y: 840), endRadius: 560, options: [])
ctx.restoreGState()
// A hairline inside the edge.
ctx.saveGState()
ctx.addPath(rounded(body.insetBy(dx: 2, dy: 2), 184))
ctx.setStrokeColor(c(0xFFFFFF, 0.2)); ctx.setLineWidth(4); ctx.strokePath()
ctx.restoreGState()

// The cards behind, fanned out to the left and fainter the further back.
let w: CGFloat = 340, h: CGFloat = 520, cx: CGFloat = 572, cy: CGFloat = 500
let shadow = c(0x7A1A2A, 0.4)
for (dx, angle, alpha) in [(CGFloat(-120), CGFloat(0.20), CGFloat(0.35)), (CGFloat(-60), CGFloat(0.10), CGFloat(0.6))] {
  ctx.saveGState()
  ctx.translateBy(x: cx + dx, y: cy); ctx.rotate(by: angle)
  shadowFill(rounded(CGRect(x: -w/2, y: -h/2, width: w, height: h), 60), c(0xFFFFFF, alpha), shadow: c(0x7A1A2A, 0.2), y: -10, blur: 24)
  ctx.restoreGState()
}

// The front card.
let card = CGRect(x: cx - w/2, y: cy - h/2, width: w, height: h)
shadowFill(rounded(card, 60), c(0xFFFFFF), shadow: shadow, y: -22, blur: 44)

// Play, a rounded triangle in the middle of the front card.
let t: CGFloat = 104, tx = card.midX + t * 0.15, ty = card.midY
let a = CGPoint(x: tx - t*0.62, y: ty + t*0.86), b = CGPoint(x: tx + t*0.92, y: ty), d = CGPoint(x: tx - t*0.62, y: ty - t*0.86)
let play = CGMutablePath()
let rr: CGFloat = 24
play.move(to: CGPoint(x: (a.x+d.x)/2, y: (a.y+d.y)/2))
play.addArc(tangent1End: a, tangent2End: b, radius: rr)
play.addArc(tangent1End: b, tangent2End: d, radius: rr)
play.addArc(tangent1End: d, tangent2End: a, radius: rr)
play.closeSubpath()
ctx.saveGState()
ctx.addPath(play); ctx.clip()
ctx.drawLinearGradient(grad([c(0xFF8A3D), c(0xF2366F)], [0, 1]), start: CGPoint(x: card.minX, y: card.maxY), end: CGPoint(x: card.maxX, y: card.minY), options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
ctx.restoreGState()

let img = ctx.makeImage()!
let rep = NSBitmapImageRep(cgImage: img)
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
