# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Docker container images providing a ready-to-use development environment for [PicoClaw](https://github.com/sipeed/picoclaw) — an AI agent platform supporting multiple messaging channels and 20+ AI model providers. Published to GHCR as two variants:

- **`ghcr.io/picoaide/picoaide-browser`** — full image with Chromium (default/recommended)
- **`ghcr.io/picoaide/picoaide`** — slim image without browser

Both are built from the same Dockerfile using the `INSTALL_BROWSER` build arg.

## Build Commands

```bash
# Build with browser (default recommended)
docker build --build-arg INSTALL_BROWSER=true -t picoaide-browser .

# Build without browser (slim)
docker build --build-arg INSTALL_BROWSER=false -t picoaide .

# Run with docker-compose
docker compose up -d

# Enter the running container
docker exec -it picoaide-deploy zsh
```

There are no tests or linting — the project consists of a Dockerfile, entrypoint script, and CI workflow.

## Architecture

**Four files define the entire project:**

- **`Dockerfile`** — Debian 13 based image with `INSTALL_BROWSER` build arg. Installs system packages, SSH (key-only auth), uv, NVM + Node.js v22 LTS, and downloads the latest PicoClaw binary from GitHub releases. When `INSTALL_BROWSER=true`, also installs Chromium. Configures China-friendly mirrors (Tsinghua) for apt/npm/pip. Creates a `/root.original` backup for first-mount initialization.
- **`entrypoint.sh`** — Configures NVM for zsh, restores `/root` from backup on first volume mount, then runs `picoclaw gateway -E` as the main process.
- **`docker-compose.yaml`** — Compose file using the browser image by default, mounting `./root` → `/root` and `./data` → `/data`, exposing port 2222 for SSH.
- **`.github/workflows/docker.yml`** — CI/CD pipeline: triggers on push to main, manual dispatch, and daily schedule (UTC 03:00). Uses a `check-version` job to detect the latest PicoClaw release and skip if already built, then a matrix `build` job producing both `picoaide` and `picoaide-browser` images for amd64/arm64.

## Key Design Decisions

- **Single Dockerfile, dual variants** — the `INSTALL_BROWSER` build arg controls whether Chromium is included. Both variants share all other layers, differing only in the browser installation step.
- **PicoClaw version detection** happens at `docker build` time, downloading whichever release is latest from the GitHub API. The CI workflow only checks whether a rebuild is needed.
- **Node.js version is hardcoded** in the Dockerfile `ENV PATH` (currently v22.12.0). When NVM's LTS changes, this path must be updated manually.
- **First-mount init**: When `/root` is empty (new volume), `entrypoint.sh` copies from `/root.original` to seed shell configs and NVM setup, then truncates `authorized_keys` for security.
- **The image doubles as an SSH server**: SSH is configured for key-only auth, useful for remote access without `docker exec`.
- **`.dockerignore` excludes `root/`** to prevent accidentally shipping host data into the image.

## CI/CD

The workflow uses a two-job structure: `check-version` determines if a new PicoClaw release exists, then `build` runs a matrix for both variants. Each variant gets tagged with `latest`, `vX.Y.Z`, `X.Y`, and commit SHA. Builds use GitHub Actions cache (`type=gha`) and QEMU + Buildx for multi-platform support.
