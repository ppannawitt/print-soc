Print @ SoC for macOS 13 or later, with one universal app for Apple Silicon and Intel.

- PDF preview, page selection, scaling, orientation, and pages per sheet.
- Printer directory, local print history, live queues, and owned-job cancellation.
- Green interface, system appearance, and native macOS alerts.
- Local SSH-key creation and public-key copying. Register keys manually with SoC; automatic registration is removed.

Download `Print-at-SoC-Universal.dmg`, verify its SHA-256 checksum, and drag the app into Applications. **Signing status:** ad-hoc signed, not Developer ID signed and not notarized by Apple. macOS may require approval to open the app.

[Printing guide](https://ppannawitt.github.io/print-soc/printing.html) · [Manual SSH-key guide](https://ppannawitt.github.io/print-soc/ssh-keys.html)

The maintainer reports thorough testing and has authorized this unsigned v1 release. Automated checks passed; detailed hardware/accessibility coverage has not been independently verified.

An independent student project, not an official NUS application. A SoC account and Unix access are required. PDFs are uploaded to SoC for printing. Credentials and history stay on the Mac; there is no telemetry. A successful submission does not confirm physical printing. Check the queue before repeating a job with an unknown outcome.
