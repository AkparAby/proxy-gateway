# Windows application icon

`icon.png` is the source artwork. It was created with the built-in imagegen tool
for this project: a mint gateway with one input and multiple outputs on a navy
tile, with transparency outside its rounded corners. It is covered by the
project's MIT license.

`rsrc_windows_amd64.syso` in the repository root is the Windows resource object.
Go embeds it automatically during a normal Windows amd64 build. The resource
contains 16, 32, 48, 64 and 256 pixel icon sizes, so running the EXE does not need
an external icon file.

`installer.ico` contains the same multi-size artwork for the NSIS installer and
uninstaller. Regenerate it alongside the executable resources when changing
the application icon.

To regenerate after changing the artwork, run from the repository root:

```powershell
go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64
```

The resource generator is a development tool, not a runtime dependency. Its
[upstream documentation](https://github.com/tc-hib/go-winres) describes the JSON
format and PNG-to-icon conversion.

Original image generation prompt:

> Create one finished Windows desktop application icon for a local proxy gateway named Proxy Gateway. No text or letters. A simple bold geometric gateway / routing symbol: one central mint green portal or hub with three clean branching connections and round terminal nodes, expressing one entry and multiple network exits. Confident, friendly utility app aesthetic, crisp flat vector-like geometry, thick shapes readable at 16 and 32 pixels. A deep navy rounded-square tile behind the mint and ivory symbol, centered with a small transparent margin outside its rounded corners. Palette deep navy #101c29, mint #6bd3b5, subtle ivory accents. Straight-on 2D, perfectly square image, strong silhouette, restrained detail, no perspective, no mockup, no photographic textures, no soft glow, no thin hairlines, no drop shadow outside tile, no border or watermark. Actual transparency outside the rounded tile. Output a single icon, not a sheet of variants.
