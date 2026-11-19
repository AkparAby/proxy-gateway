# Third-party notices

The Proxy Gateway manager, dashboard and integration scripts are licensed under
the MIT License in `LICENSE`. The following independently distributed components
retain their own licenses; the project's MIT license does not replace them.

## Mihomo

- Project: <https://github.com/MetaCubeX/mihomo>
- Copyright: the Mihomo project and its contributors; see the source archive for
  the original copyright notices and dependency licenses.
- Version: `v1.19.33`, commit `3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8`.
- License: GNU General Public License version 3. The Windows package includes
  the upstream license in `licenses/Mihomo-GPL-3.0.txt`; the macOS app stores it
  in `Contents/Resources/licenses/Mihomo-GPL-3.0.txt`.
- Windows binary: the unmodified official `mihomo-windows-amd64-v1-v1.19.33.zip`,
  extracted and renamed to `runtime/mihomo.exe`.
- macOS binary: the official `mihomo-darwin-arm64-v1.19.33.gz`, extracted to
  `Proxy Gateway.app/Contents/Resources/mihomo` and ad-hoc re-signed for the app
  bundle. Its program code is not modified. Both platforms run the core as a
  separate process; archive hashes are pinned in `scripts/mihomo.lock.json`.
- Corresponding source: `mihomo-v1.19.33-source.zip` is available alongside the
  Windows and macOS packages on the **same [GitHub Release](https://github.com/AkparAby/proxy-gateway/releases)**,
  at no charge. This archive contains the exact upstream source, dependency
  manifests, build scripts and license notices. `release-manifest.json` in the
  portable package records direct source and binary download URLs and hashes.
- Upstream source: <https://github.com/MetaCubeX/mihomo/tree/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8>.

## Go

The manager and Mihomo are built with Go and include its runtime and standard
library. Copyright belongs to The Go Authors. The Go BSD-style license is
included in `licenses/Go-BSD.txt` on Windows and
`Proxy Gateway.app/Contents/Resources/licenses/Go-BSD.txt` on macOS.

## Fyne desktop and other Go modules

The desktop UI uses `fyne.io/fyne/v2` version `v2.8.2` and its Go dependencies.
Fyne's BSD-3-Clause license and the license/copyright notices collected from
the linked dependency modules are included in `licenses/Go-dependencies.txt`
(inside `Contents/Resources/` for macOS). The exact dependency versions and
checksums are pinned in `go.mod` and `go.sum`.

## Noto Sans CJK SC

The desktop embeds the unmodified Noto Sans CJK SC Regular font for Chinese
text. It is distributed under the SIL Open Font License 1.1, included in
`licenses/NotoSansSC-OFL.txt`. Its official source URL, commit and SHA-256 are
recorded in `assets/font-source.json` in this repository.

## Windows graphics toolchain

Windows desktop builds use MinGW-w64 through w64devkit. The MinGW-w64 runtime
notice is included in `licenses/MinGW-w64-runtime.txt`. The compiler itself
is a build-time tool and is not bundled with the application.

## NSIS Windows installer

The Windows Setup.exe and generated uninstaller contain the NSIS runtime and
use its zlib compression module, under the zlib/libpng license. The notice is
included in `licenses/NSIS-zlib.txt`. The compiler is used only at build time;
its toolchain provenance is recorded in `scripts/windows-toolchain.json`.

The package does not include SpeedCat, Clash Verge, subscriptions, accounts or
proxy nodes. Users install and configure their own upstream clients separately.
