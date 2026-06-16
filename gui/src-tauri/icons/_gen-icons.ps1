# Generates Hull app icons (PNG set + multi-size ICO) from the logo geometry.
# The logo is two flat polygons in an 86x90 viewBox — drawn here with GDI+
# so we need no external SVG rasterizer. Run from any cwd.
Add-Type -AssemblyName System.Drawing

$outDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# Logo vertices in the 86x90 viewBox (sub-pixel corner radii dropped).
$grey = @(86,89, 85,90, 59,90, 58,89, 58,57, 57,56, 44,56, 43,55, 43,33, 44,32, 57,32, 58,31, 58,1, 59,0, 85,0, 86,1)
$gold = @(28,31, 29,32, 42,32, 43,33, 43,55, 42,56, 29,56, 28,57, 28,89, 27,90, 1,90, 0,89, 0,1, 1,0, 27,0, 28,1)
$greyColor = [System.Drawing.Color]::FromArgb(255, 0xCF, 0xDB, 0xD5)
$goldColor = [System.Drawing.Color]::FromArgb(255, 0xF5, 0xCB, 0x5C)
$vbW = 86.0; $vbH = 90.0; $pad = 0.14

function Poly([double[]]$flat, [double]$scale, [double]$ox, [double]$oy) {
  $pts = New-Object 'System.Collections.Generic.List[System.Drawing.PointF]'
  for ($i = 0; $i -lt $flat.Length; $i += 2) {
    $pts.Add([System.Drawing.PointF]::new([single]($flat[$i] * $scale + $ox), [single]($flat[$i+1] * $scale + $oy)))
  }
  return $pts.ToArray()
}

function Render([int]$size) {
  $bmp = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
  $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $g.Clear([System.Drawing.Color]::Transparent)
  $scale = ($size * (1 - 2 * $pad)) / [Math]::Max($vbW, $vbH)
  $ox = ($size - $vbW * $scale) / 2
  $oy = ($size - $vbH * $scale) / 2
  $bGrey = New-Object System.Drawing.SolidBrush($greyColor)
  $bGold = New-Object System.Drawing.SolidBrush($goldColor)
  $g.FillPolygon($bGrey, (Poly $grey $scale $ox $oy))
  $g.FillPolygon($bGold, (Poly $gold $scale $ox $oy))
  $g.Dispose(); $bGrey.Dispose(); $bGold.Dispose()
  return $bmp
}

function PngBytes($bmp) {
  $ms = New-Object System.IO.MemoryStream
  $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
  return $ms.ToArray()
}

# PNG set Tauri references / embeds.
$pngSizes = @{ "32x32.png" = 32; "128x128.png" = 128; "128x128@2x.png" = 256; "icon.png" = 512 }
foreach ($name in $pngSizes.Keys) {
  $bmp = Render $pngSizes[$name]
  $bmp.Save((Join-Path $outDir $name), [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
}

# Multi-size ICO with PNG-compressed entries (16/32/48/64/256).
$icoSizes = @(16, 32, 48, 64, 256)
$sizes = [System.Collections.Generic.List[int]]::new()
$datas = [System.Collections.Generic.List[byte[]]]::new()
foreach ($s in $icoSizes) {
  $bmp = Render $s
  $sizes.Add($s)
  $datas.Add([byte[]](PngBytes $bmp))
  $bmp.Dispose()
}

$count = $sizes.Count
$ico = New-Object System.IO.MemoryStream
$bw = New-Object System.IO.BinaryWriter($ico)
$bw.Write([uint16]0); $bw.Write([uint16]1); $bw.Write([uint16]$count)  # ICONDIR
$offset = 6 + 16 * $count
for ($i = 0; $i -lt $count; $i++) {
  $dim = if ($sizes[$i] -ge 256) { 0 } else { $sizes[$i] }
  $len = $datas[$i].Length
  $bw.Write([byte]$dim); $bw.Write([byte]$dim)           # width, height
  $bw.Write([byte]0); $bw.Write([byte]0)                 # colors, reserved
  $bw.Write([uint16]1); $bw.Write([uint16]32)            # planes, bpp
  $bw.Write([uint32]$len)                                # bytes in resource
  $bw.Write([uint32]$offset)                             # offset
  $offset += $len
}
for ($i = 0; $i -lt $count; $i++) { $bw.Write($datas[$i], 0, $datas[$i].Length) }
$bw.Flush()
[System.IO.File]::WriteAllBytes((Join-Path $outDir "icon.ico"), $ico.ToArray())
$bw.Dispose()

Write-Output "Done. Icons written to $outDir"
