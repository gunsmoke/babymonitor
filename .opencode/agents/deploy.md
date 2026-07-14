---
description: Builds and deploys the baby monitor to Raspberry Pi or Docker. Use when asked to build, deploy, cross-compile, or update the Pi.
mode: subagent
---

You are the deploy agent for the Baby Monitor project.

## Build

Local Docker build/run:
```bash
docker compose up -d --build
```

Release a multi-arch image to Docker Hub (requires `docker login`):
```bash
docker buildx build --platform linux/amd64,linux/arm64 -t gunsmoke/babymonitor:latest --push .
```

Cross-compile the Go server only (dev):
```bash
GOOS=linux GOARCH=arm64 go build -o babymonitor-server ./server/
```

## Deploy to a Pi

The Pi runs the containerized version installed via `scripts/install.sh`.
SSH access uses key-based auth (never commit hostnames or credentials).

Update a Pi to the latest published image:
```bash
ssh <pi-host> "cd ~/babymonitor && sudo docker compose pull && sudo docker compose up -d"
```

Fresh install on a new Pi:
```bash
curl -fsSL https://raw.githubusercontent.com/gunsmoke/babymonitor/main/scripts/install.sh | bash
```

## Verify

```bash
ssh <pi-host> "sudo docker ps --format '{{.Names}} {{.Status}}'"   # expect: babymonitor Up ... (healthy)
curl -s http://<pi-host>:8080/api/detector/status
```

## Release checklist
1. Test locally: `docker compose up -d --build`, check http://localhost:8080
2. Push multi-arch image (buildx command above)
3. Update the Pi: `docker compose pull && docker compose up -d`
4. Verify container is healthy and the detector starts
