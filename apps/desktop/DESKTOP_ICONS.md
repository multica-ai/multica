# Forge Desktop Icon Generation

The source icon is `apps/desktop/resources/icon.png` (512×512 PNG, Asymbl brand mark).

To regenerate platform-specific formats:

## macOS (.icns)
```bash
# Requires macOS with Xcode command line tools
mkdir forge.iconset
sips -z 16 16     resources/icon.png --out forge.iconset/icon_16x16.png
sips -z 32 32     resources/icon.png --out forge.iconset/icon_16x16@2x.png
sips -z 32 32     resources/icon.png --out forge.iconset/icon_32x32.png
sips -z 64 64     resources/icon.png --out forge.iconset/icon_32x32@2x.png
sips -z 128 128   resources/icon.png --out forge.iconset/icon_128x128.png
sips -z 256 256   resources/icon.png --out forge.iconset/icon_128x128@2x.png
sips -z 256 256   resources/icon.png --out forge.iconset/icon_256x256.png
sips -z 512 512   resources/icon.png --out forge.iconset/icon_256x256@2x.png
sips -z 512 512   resources/icon.png --out forge.iconset/icon_512x512.png
iconutil -c icns forge.iconset -o resources/icon.icns
```

## Windows (.ico)
```bash
# Requires ImageMagick
magick resources/icon.png -resize 256x256 resources/icon.ico
```

Run these commands from the `apps/desktop/` directory before building a production release.
