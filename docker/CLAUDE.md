# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Docker container image providing a ready-to-use development environment for [PicoClaw](https://github.com/sipeed/picoclaw) — an AI agent platform supporting multiple messaging channels and 20+ AI model providers. Published to GHCR as `ghcr.io/picoaide/picoaide`.

## Build Commands

```bash
# Build with specific version (required)
docker build --build-arg PICOCLAW_VERSION=v0.2.6 -t picoaide .

# Run the container
docker run -d --name picoaide-deploy -v picoaide-root:/root picoaide

# Enter the running container
docker exec -it picoaide-deploy zsh
```

There are no tests or linting — the project consists of a Dockerfile, entrypoint script, and CI workflow.

## Architecture

**Four files define the entire project:**

- **`Dockerfile`** — Debian 13 based image. Installs system packages, uv, NVM + Node.js v22 LTS, and downloads a specific version of PicoClaw binary from GitHub releases. Configures China-friendly mirrors (Tsinghua) for apt/npm/pip. Creates a `/root.original` backup for first-mount initialization.
- **`entrypoint.sh`** — Configures NVM for zsh, restores `/root` from backup on first volume mount, then runs `picoclaw gateway -E` as the main process.
- **`.github/workflows/docker.yml`** — CI/CD pipeline: triggers on push to main, manual dispatch, and daily schedule (UTC 03:00). Uses a `check-version` job to detect the latest PicoClaw release and skip if already built, then builds with fixed version tag (no `latest`).

## Key Design Decisions

- **Fixed version tags only** — no `latest` tag is published. Every image is tagged with the exact PicoClaw version (e.g., `v0.2.6`). `PICOCLAW_VERSION` build arg is required.
- **PicoClaw version detection** happens in CI via `check-version` job, not at `docker build` time.
- **Node.js version is hardcoded** in the Dockerfile `ENV PATH` (currently v22.12.0). When NVM's LTS changes, this path must be updated manually.
- **First-mount init**: When `/root` is empty (new volume), `entrypoint.sh` copies from `/root.original` to seed shell configs and NVM setup.
- **`.dockerignore` excludes `root/`** to prevent accidentally shipping host data into the image.

## CI/CD

The workflow uses a two-job structure: `check-version` determines if a new PicoClaw release exists, then `build` produces a single `ghcr.io/picoaide/picoaide` image tagged with the version number only. Builds use GitHub Actions cache (`type=gha`) and QEMU + Buildx for multi-platform (amd64/arm64) support.
