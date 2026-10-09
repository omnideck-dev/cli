#!/usr/bin/env python3
"""Package the final signed Mach-O bytes with stable archive metadata."""
import gzip
import sys
import tarfile

binary, output, epoch = sys.argv[1:]
with open(output, "wb") as destination:
    with gzip.GzipFile(filename="", mode="wb", fileobj=destination, mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
            info = archive.gettarinfo(binary, arcname="omnideck")
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            info.mode = 0o755
            info.mtime = int(epoch)
            with open(binary, "rb") as source:
                archive.addfile(info, source)
