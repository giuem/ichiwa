// Draws the app icon: swift scripts/icon.swift resources/icon.png
// then: sips -z 128 128 resources/icon.png --out internal/app/assets/logo.png
import AppKit

let S: CGFloat = 1024
let cs = CGColorSpace(name: CGColorSpace.displayP3)!
let ctx = CGContext(data: nil, width: Int(S), height: Int(S), bitsPerComponent: 8, bytesPerRow: 0, space: cs, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
func c(_ hex: UInt32, _ a: CGFloat = 1) -> CGColor {
  CGColor(colorSpace: cs, components: [CGFloat((hex>>16)&255)/255, CGFloat((hex>>8)&255)/255, CGFloat(hex&255)/255, a])!
}
func grad(_ cols: [CGColor], _ locs: [CGFloat]) -> CGGradient { CGGradient(colorsSpace: cs, colors: cols as CFArray, locations: locs)! }

// Body: the macOS icon grid, 824 points with rounded corners.
let body = CGRect(x: 100, y: 100, width: 824, height: 824)
let bodyPath = CGPath(roundedRect: body, cornerWidth: 186, cornerHeight: 186, transform: nil)
ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 0, height: -14), blur: 28, color: c(0x000000, 0.28))
ctx.addPath(bodyPath); ctx.setFillColor(c(0xE8132F)); ctx.fillPath()
ctx.restoreGState()
ctx.saveGState()
ctx.addPath(bodyPath); ctx.clip()
ctx.drawLinearGradient(grad([c(0xFF5A5F), c(0xF5223D), c(0xD80F2E)], [0, 0.55, 1]), start: CGPoint(x: 300, y: 924), end: CGPoint(x: 724, y: 100), options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
// A soft light from the top left.
ctx.drawRadialGradient(grad([c(0xFFFFFF, 0.28), c(0xFFFFFF, 0)], [0, 1]), startCenter: CGPoint(x: 330, y: 820), startRadius: 0, endCenter: CGPoint(x: 330, y: 820), endRadius: 520, options: [])
ctx.restoreGState()
// A hairline inside the edge.
ctx.saveGState()
ctx.addPath(CGPath(roundedRect: body.insetBy(dx: 2, dy: 2), cornerWidth: 184, cornerHeight: 184, transform: nil))
ctx.setStrokeColor(c(0xFFFFFF, 0.18)); ctx.setLineWidth(4); ctx.strokePath()
ctx.restoreGState()

// The fruit: round, with a dip at the top where the stem goes in.
let cx: CGFloat = 512, cy: CGFloat = 470, r: CGFloat = 262
let fruit = CGMutablePath()
fruit.move(to: CGPoint(x: cx, y: cy + r - 34))
fruit.addCurve(to: CGPoint(x: cx + r, y: cy + 40), control1: CGPoint(x: cx + 70, y: cy + r + 26), control2: CGPoint(x: cx + r, y: cy + r - 20))
fruit.addCurve(to: CGPoint(x: cx, y: cy - r), control1: CGPoint(x: cx + r, y: cy - r*0.62), control2: CGPoint(x: cx + r*0.55, y: cy - r))
fruit.addCurve(to: CGPoint(x: cx - r, y: cy + 40), control1: CGPoint(x: cx - r*0.55, y: cy - r), control2: CGPoint(x: cx - r, y: cy - r*0.62))
fruit.addCurve(to: CGPoint(x: cx, y: cy + r - 34), control1: CGPoint(x: cx - r, y: cy + r - 20), control2: CGPoint(x: cx - 70, y: cy + r + 26))
fruit.closeSubpath()

// Stem.
ctx.saveGState()
let stem = CGMutablePath()
stem.move(to: CGPoint(x: cx, y: cy + r - 50))
stem.addCurve(to: CGPoint(x: cx + 22, y: cy + r + 92), control1: CGPoint(x: cx + 4, y: cy + r + 20), control2: CGPoint(x: cx + 6, y: cy + r + 60))
ctx.addPath(stem); ctx.setLineCap(.round); ctx.setLineWidth(30); ctx.setStrokeColor(c(0x6B3A1E)); ctx.strokePath()
ctx.restoreGState()

// Leaf.
ctx.saveGState()
ctx.translateBy(x: cx + 118, y: cy + r + 70)
ctx.rotate(by: 0.42)
let leaf = CGMutablePath()
leaf.move(to: CGPoint(x: -118, y: 0))
leaf.addCurve(to: CGPoint(x: 118, y: 0), control1: CGPoint(x: -60, y: 92), control2: CGPoint(x: 60, y: 92))
leaf.addCurve(to: CGPoint(x: -118, y: 0), control1: CGPoint(x: 60, y: -80), control2: CGPoint(x: -60, y: -80))
leaf.closeSubpath()
ctx.setShadow(offset: CGSize(width: 0, height: -6), blur: 14, color: c(0x000000, 0.2))
ctx.addPath(leaf); ctx.setFillColor(c(0x2FB35A)); ctx.fillPath()
ctx.setShadow(offset: .zero, blur: 0, color: nil)
ctx.addPath(leaf); ctx.clip()
ctx.drawLinearGradient(grad([c(0x9BE15D), c(0x2DB45A)], [0, 1]), start: CGPoint(x: 0, y: 90), end: CGPoint(x: 0, y: -80), options: [])
// The vein.
ctx.resetClip()
let vein = CGMutablePath(); vein.move(to: CGPoint(x: -96, y: 2)); vein.addQuadCurve(to: CGPoint(x: 92, y: 6), control: CGPoint(x: 0, y: 22))
ctx.addPath(vein); ctx.setStrokeColor(c(0xFFFFFF, 0.45)); ctx.setLineWidth(7); ctx.setLineCap(.round); ctx.strokePath()
ctx.restoreGState()

// Fruit body, white with a blush, over a soft shadow.
ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 0, height: -18), blur: 36, color: c(0x7A0013, 0.35))
ctx.addPath(fruit); ctx.setFillColor(c(0xFFFFFF)); ctx.fillPath()
ctx.restoreGState()
ctx.saveGState()
ctx.addPath(fruit); ctx.clip()
ctx.drawLinearGradient(grad([c(0xFFFFFF), c(0xFFF1F3), c(0xFFD9DF)], [0, 0.6, 1]), start: CGPoint(x: cx, y: cy + r), end: CGPoint(x: cx, y: cy - r), options: [])
ctx.drawRadialGradient(grad([c(0xFFFFFF, 0.9), c(0xFFFFFF, 0)], [0, 1]), startCenter: CGPoint(x: cx - 120, y: cy + 120), startRadius: 0, endCenter: CGPoint(x: cx - 120, y: cy + 120), endRadius: 160, options: [])
ctx.restoreGState()

// Play, a rounded triangle in the fruit.
let t: CGFloat = 150, tx = cx + 24, ty = cy - 6
let a = CGPoint(x: tx - t*0.62, y: ty + t*0.86), b = CGPoint(x: tx + t*0.92, y: ty), d = CGPoint(x: tx - t*0.62, y: ty - t*0.86)
let play = CGMutablePath()
let rr: CGFloat = 34
play.move(to: CGPoint(x: (a.x+d.x)/2, y: (a.y+d.y)/2))
play.addArc(tangent1End: a, tangent2End: b, radius: rr)
play.addArc(tangent1End: b, tangent2End: d, radius: rr)
play.addArc(tangent1End: d, tangent2End: a, radius: rr)
play.closeSubpath()
ctx.saveGState()
ctx.addPath(play); ctx.clip()
ctx.drawLinearGradient(grad([c(0xFF4B5C), c(0xE0102E)], [0, 1]), start: CGPoint(x: tx, y: ty + t), end: CGPoint(x: tx, y: ty - t), options: [])
ctx.restoreGState()

let img = ctx.makeImage()!
let rep = NSBitmapImageRep(cgImage: img)
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
