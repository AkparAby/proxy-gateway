# Desktop resources

- `icon.png`: the project's MIT-licensed icon, shared with the Windows icon source in `winres/`.
- `NotoSansSC-Regular.otf`: unmodified Noto Sans CJK SC Regular from the official
  [Noto CJK project](https://github.com/notofonts/noto-cjk), embedded so Chinese
  text renders consistently on Windows and macOS. Its exact source commit, URL
  and SHA-256 are in `font-source.json`; the upstream SIL Open Font License and
  copyright notice are in `NotoSansSC-OFL.txt`.
- `MinGW-w64-runtime.txt`: MinGW-w64 runtime license notice from w64devkit
  2.11.0, retained for statically linked Windows runtime support.

The desktop UI is written in Go with Fyne. Building the native desktop requires
CGO and the platform graphics toolchain; the background manager remains
independent of the graphics driver. The `desktop` build tag selects the desktop
code, and `desktop,ci` selects Fyne's in-memory driver for UI tests.
