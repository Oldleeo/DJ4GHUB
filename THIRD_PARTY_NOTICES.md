# Third-Party Notices

DJ 4G Hub contains code derived from the upstream VoHive project and work inspired by ZenGeekLabs/DJOneHub. It retains the license and required notice provided in the repository root [`LICENSE`](LICENSE):

```text
Required Notice: Copyright iniwex5 (https://github.com/iniwex5/vohive)
```

## Windows Public-Source Preparation

This export targets Windows x64 and the Go relay. It does not contain a packaged
macOS native client or a libusb binary. Oldlee additions and branding:
Copyright 2026 老李 Oldlee — https://x.com/oldleeoo . This does not replace
upstream copyright or the repository's PolyForm Noncommercial license.

## Historical macOS Release Runtime

Upstream macOS packaging used **libusb 1.0.30**, distributed under the GNU Lesser General Public License, version 2.1 or later. The following is an upstream packaging notice, not a claim that this Windows export distributes that binary.

- Project: <https://libusb.info/>
- Source: <https://github.com/libusb/libusb/releases/tag/v1.0.30>
- License text in the release package: `licenses/libusb-COPYING`

## Vendored Source Dependencies

The source repository includes vendored dependencies under `third_party/` so the versions used by DJ 4G Hub remain reproducible. Their original copyright notices and license texts are retained in the corresponding directories.

| Component | License file |
| --- | --- |
| euicc-go | `third_party/euicc-go/LICENSE` |
| uicc-go | `third_party/uicc-go/LICENSE` |
| quectel-qmi-go | `third_party/quectel-qmi-go/LICENSE` |
| strftime | `third_party/strftime/LICENSE` |
| pkg/errors | `third_party/pkg-errors/LICENSE` |
| golang.org/x/sys | `third_party/x-sys/LICENSE` |
| golang.org/x/text | `third_party/x-text/LICENSE` |
| multierr | `third_party/multierr/LICENSE.txt` |
| jsQR 1.4.0 | `third_party/jsqr/LICENSE` |

The cloud console also bundles selected SVG icons from **Tabler Icons**,
distributed under the MIT License.

- Project: <https://github.com/tabler/tabler-icons>
- License text: `third_party/tabler-icons/LICENSE`

Dependencies fetched through Go modules retain their own licenses and copyright notices. This file is informational and does not replace any component's full license text.

## Windows Binary Packages

Windows installer and portable releases retain this notice, the project license,
Go runtime license, notices from the modules linked into the backend, embedded
asset licenses, Microsoft WebView2 SDK notices and .NET distribution notices in
`licenses/`. `licenses/COMPONENTS.txt` lists the resolved modules/packages.
These files are copied from the original build dependencies, not substituted
with a new license. The installer includes a Microsoft-signed Evergreen
WebView2 bootstrapper and uses it only when the Runtime is absent; its download
and installation remain subject to Microsoft's applicable terms.
