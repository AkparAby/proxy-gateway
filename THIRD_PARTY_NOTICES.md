# Third-party notices

The Proxy Gateway manager, dashboard and integration scripts are licensed under
the MIT License in `LICENSE`. The following independently distributed components
retain their own licenses; the project's MIT license does not replace them.

## Mihomo

- Project: <https://github.com/MetaCubeX/mihomo>
- Copyright: the Mihomo project and its contributors; see the source archive for
  the original copyright notices and dependency licenses.
- Version: `v1.19.33`, commit `3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8`.
- License: GNU General Public License version 3. The portable package includes
  the upstream license in `licenses/Mihomo-GPL-3.0.txt`.
- Binary: the unmodified official `mihomo-windows-amd64-v1-v1.19.33.zip`, extracted
  and renamed to `runtime/mihomo.exe`. It runs as a separate process.
- Corresponding source: `mihomo-v1.19.33-source.zip` is available alongside the
  Windows package on the **same [GitHub Release](https://github.com/AkparAby/proxy-gateway/releases)**,
  at no charge. This archive contains the exact upstream source, dependency
  manifests, build scripts and license notices. `release-manifest.json` in the
  portable package records direct source and binary download URLs and hashes.
- Upstream source: <https://github.com/MetaCubeX/mihomo/tree/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8>.

## Go

The manager and Mihomo are built with Go and include its runtime and standard
library. Copyright belongs to The Go Authors. The Go BSD-style license is
included in `licenses/Go-BSD.txt` in the portable package.

The package does not include SpeedCat, Clash Verge, subscriptions, accounts or
proxy nodes. Users install and configure their own upstream clients separately.
