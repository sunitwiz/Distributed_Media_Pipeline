# How to Start RenderMesh

## Prerequisites

- Docker and Docker Compose installed
- ffmpeg installed locally (only needed for the demo script to generate test files)

## Start the Stack

```bash
cd Distributed_Media_Pipeline

# Build and start all services (gateway + 2 workers + redis + minio)
docker compose up --build --scale worker=2
```

Add `-d` to run in detached mode (background):

```bash
docker compose up --build --scale worker=2 -d
```

## Verify Everything is Running

```bash
# Check all containers are up
docker compose ps

# Health check
curl http://localhost:8080/healthz

# List active workers
curl http://localhost:8080/api/v1/workers
```

## Run the Demo

```bash
cd scripts
bash demo.sh
```

This will auto-generate a test video and subtitle file, then run all 3 job types (transcode, extract, overlay) end-to-end.

## Run Jobs Manually

Step 1 -- Submit a transcode job:
```bash
curl -X POST http://localhost:8080/api/v1/jobs/transcode \
  -H "Content-Type: application/json" \
  -d '{"input_filename": "video.mp4"}'
```

Step 2 -- Upload your file using the upload_url from the response:
```bash
curl -X PUT "<upload_url_from_step1>" --upload-file ./video.mp4
```

Step 2.5 -- Confirm the upload:
```bash
curl -X POST http://localhost:8080/api/v1/jobs/<job_id>/confirm
```

Step 3 -- Poll for status:
```bash
curl http://localhost:8080/api/v1/jobs/<job_id>
```

Step 4 -- Download result (when status is "completed"):
```bash
curl -o output.mp4 "<download_url_from_status>"
```

## Scale Workers

```bash
# Scale to 5 workers
docker compose up --scale worker=5 -d

# Scale back down to 1
docker compose up --scale worker=1 -d
```

## View Logs

```bash
# All services
docker compose logs -f

# Gateway only
docker compose logs -f gateway

# Workers only
docker compose logs -f worker
```

## Stop Everything

```bash
docker compose down
```

To also remove stored data (Redis + MinIO volumes):
```bash
docker compose down -v
```

## Ports

| Service | Port | URL |
|---------|------|-----|
| Gateway API | 8080 | http://localhost:8080 |
| MinIO API | 9000 | http://localhost:9000 |
| MinIO Console | 9001 | http://localhost:9001 (login: minioadmin/minioadmin) |
| Redis | 6379 | redis://localhost:6379 |-->docker exec distributed_media_pipeline-redis-1 redis-cli KEYS '*'
