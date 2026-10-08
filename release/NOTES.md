SimplyPrint @ SoC v1.0.1 for macOS 13 or later, on Apple Silicon and Intel.

- Corrects a packaging error in v1.0.0 that made the executable require macOS 26 despite advertising macOS 13 support.
- Renames the app, window, menus, About panel and disk image to SimplyPrint @ SoC.
- The DMG contains only `SimplyPrint @ SoC.app`; no loose text files or Applications shortcut.
- Existing settings, Keychain credentials and print history remain compatible.

Download `SimplyPrint-at-SoC-Universal.dmg`, open it and drag the app into your Applications folder in Finder. Open the app from Applications.

**Signing status:** ad-hoc signed, not Developer ID signed and not notarized by Apple. If macOS says the developer cannot be verified, first try opening the app, then go to System Settings → Privacy & Security → Open Anyway, review the app name and choose Open. Follow [Apple’s opening guide](https://support.apple.com/en-gb/102445). Do not override a malware or damaged-app warning.

[Printing guide](https://ppannawitt.github.io/print-soc/printing.html) · [Manual SSH-key setup](https://ppannawitt.github.io/print-soc/ssh-keys.html)

Printing features include PDF preview and layout settings, a printer directory, jobs, queues, and owned-job cancellation. SSH-key registration remains manual. This is an independent student project, not an official NUS application.

Both executable slices and the app bundle now declare macOS 13.0. Automated checks and a local launch are verified; older macOS and physical Intel runtime checks have not been independently verified. The maintainer has authorized publication without Apple signing. The DMG checksum and release validation record are separate release assets.
