#!/usr/bin/env swift
import AppKit

// One vector geometry for the app, sidebar and macOS template status item.
let commands: [(String, [CGFloat])] = [
    ("M", [65,25]), ("L", [34,25]), ("Q", [22,25,22,37]),
    ("L", [22,60]), ("Q", [22,72,34,72]), ("L", [36,72]),
    ("L", [30,84]), ("L", [51,72]), ("L", [66,72]),
    ("Q", [78,72,78,60]), ("L", [78,46])
]
let path = CGMutablePath()
for (op, p) in commands {
    switch op {
    case "M": path.move(to: CGPoint(x:p[0],y:p[1]))
    case "L": path.addLine(to: CGPoint(x:p[0],y:p[1]))
    case "Q": path.addQuadCurve(to: CGPoint(x:p[2],y:p[3]), control: CGPoint(x:p[0],y:p[1]))
    default: fatalError("Unknown vector operation")
    }
}
let space = CGColorSpace(name: CGColorSpace.sRGB)!
let green = CGColor(colorSpace:space,components:[63/255.0,92/255.0,70/255.0,1])!
let cream = CGColor(colorSpace:space,components:[249/255.0,248/255.0,240/255.0,1])!
func render(_ size: Int, template: Bool, to file: String) throws {
    let context = CGContext(data:nil,width:size,height:size,bitsPerComponent:8,bytesPerRow:0,
        space:space,bitmapInfo:CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.scaleBy(x:CGFloat(size)/100,y:CGFloat(size)/100)
    context.translateBy(x:0,y:100); context.scaleBy(x:1,y:-1)
    if !template {
        context.setFillColor(green)
        context.addPath(CGPath(roundedRect:CGRect(x:5,y:5,width:90,height:90),cornerWidth:20,cornerHeight:20,transform:nil))
        context.fillPath()
    }
    context.setStrokeColor(template ? CGColor(gray:0,alpha:1) : cream)
    context.setFillColor(template ? CGColor(gray:0,alpha:1) : cream)
    context.setLineWidth(template ? 6.5 : 5.5)
    context.setLineCap(.round); context.setLineJoin(.round)
    context.addPath(path);context.strokePath()
    context.fillEllipse(in:CGRect(x:71.5,y:20.5,width:13,height:13))
    let rep=NSBitmapImageRep(cgImage:context.makeImage()!)
    try rep.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:file))
}
let fm=FileManager.default
try fm.createDirectory(atPath:"build/icons/AppIcon.iconset",withIntermediateDirectories:true)
for size in [16,32,128,256,512] {
    try render(size,template:false,to:"build/icons/AppIcon.iconset/icon_\(size)x\(size).png")
    try render(size*2,template:false,to:"build/icons/AppIcon.iconset/icon_\(size)x\(size)@2x.png")
}
try render(1024,template:false,to:"build/icons/app.png")
try render(44,template:true,to:"build/icons/status-template.png")
let d=commands.map { op,p in op + p.map{String(Double($0))}.joined(separator:" ") }.joined(separator:" ")
let svg="""
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect x="5" y="5" width="90" height="90" rx="20" fill="#3f5c46"/><path d="\(d)" fill="none" stroke="#f9f8f0" stroke-width="5.5" stroke-linecap="round" stroke-linejoin="round"/><circle cx="78" cy="27" r="6.5" fill="#f9f8f0"/></svg>
"""
try svg.write(toFile:"frontend/public/popchat.svg",atomically:true,encoding:.utf8)
