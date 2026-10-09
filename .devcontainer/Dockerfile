FROM golang:1.26.9-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c

RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates git zip zsh \
    && rm -rf /var/lib/apt/lists/*
