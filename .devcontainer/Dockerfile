FROM golang:1.25.12-bookworm@sha256:6359592445455f2dbe2412bed411336035bc019a50017720d77454ffdd6d0f82

RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates git zip zsh \
    && rm -rf /var/lib/apt/lists/*
