#!/usr/bin/env python3
"""Zero-leak forensic audit for the release binary.

The proprietary vendor baseband stack (Qualcomm EFS2/PDC, Samsung SecRIL,
Google Tensor Shannon) has been fully removed from the codebase. This script
guards against accidental reintroduction by scanning a built binary for any
residual vendor package paths or endpoint strings.
"""
import sys
from pathlib import Path

# Binary signatures that must never appear in a shipped build.
TARGETS = [
    b"internal/efs",
    b"internal/shannon",
    b"internal/samsung",
    b"QualcommIMEI",
    b"diag_linux",
    b"carrier_policy",
    b"BypassSecurity",
    b"0x29 MODE_ONLINE",
    b"knownQualcommBypassPayloads",
    b"ISemTelephony",
    b"/api/efs",
    b"/api/qualcomm",
    b"/api/shannon",
    b"/api/samsung",
    b"routes_vendor_modem",
]


def audit_binary(bin_path: str) -> None:
    p = Path(bin_path)
    if not p.is_file():
        print(f"Error: binary file not found: {bin_path}", file=sys.stderr)
        sys.exit(1)

    data = p.read_bytes()

    leaks = []
    for t in TARGETS:
        cnt = data.count(t)
        if cnt > 0:
            leaks.append((t.decode("latin1", errors="replace"), cnt))

    if leaks:
        print("SECURITY AUDIT FAILED! Vendor modem signatures were found:", file=sys.stderr)
        for name, count in leaks:
            print(f"  - {name}: {count} occurrence(s)", file=sys.stderr)
        sys.exit(1)

    print(f"PASS: {bin_path} verified clean. Zero vendor modem code or strings detected.")


if __name__ == "__main__":
    target_bin = sys.argv[1] if len(sys.argv) > 1 else "webui-nomodem"
    audit_binary(target_bin)
