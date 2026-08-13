FROM golang:1.25.13-bookworm@sha256:40dfc169bd5ad8a8617e49c8ead7fe16c6873e79d6937539e9c2e5947b7984ef

RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates git zip zsh \
    && rm -rf /var/lib/apt/lists/*
