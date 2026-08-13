# Omnideck icon

`omnideck.svg` mirrors the desktop application's canonical icon source. The
generated PNG is embedded in the Windows CLI executable; `styles.BrandMark`
uses the same three blues for the terminal-safe header mark.

Linux and macOS command binaries do not carry a shell-visible application icon.
Future GUI package metadata can reuse these assets without inventing a separate
CLI identity.
