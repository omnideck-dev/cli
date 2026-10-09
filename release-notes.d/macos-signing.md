---
type: security
area: distribution
---

macOS CLI releases are signed with a Developer ID certificate and notarized by
Apple before packaging. The release workflow verifies the signed downloads on
Apple Silicon and Intel Macs. The Go toolchain is updated to 1.26.9 to address
standard-library vulnerabilities reported by the release security checks.
