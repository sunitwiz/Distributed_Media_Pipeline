# RenderMesh -- Design Document

## 1. Overview

RenderMesh is a distributed media processing engine that accepts user-submitted video jobs, queues them, dispatches them to a pool of FFmpeg worker nodes, and returns results. The entire system runs inside Docker Compose.

The system supports three job types:

- **Overlay**: Burn an `.srt` subtitle file into a video
- **Transcode**: Downscale a video to 480p
- **Extract**: Strip audio from a video and encode it as MP3

---

## 2. Architecture

### Components

| Component | Technology | Role |
|-----------|-----------|------|
| API Gateway | Go (Gin) | Central entry point for job submission, status polling, and worker visibility |
| Redis | Redis 7 | Job state store (hashes), job queue (list), worker registry (sorted set) |
| MinIO | MinIO (S3-compatible) | Object store for input and output media files |
| Worker | Go + FFmpeg | Stateless, horizontally scalable job processors |

### How They Connect

All components run on a shared Docker Compose network. The Gateway exposes port 8080 to the host. MinIO exposes ports 9000 (API) and 9001 (console). Redis and workers are internal-only.

- **Client <-> Gateway**: Small JSON requests/responses (job metadata, status). No media bytes pass through the Gateway.
- **Client <-> MinIO**: Large file uploads/downloads via pre-signed URLs. Direct client-to-storage transfer.
- **Gateway <-> Redis**: Job record CRUD, queue push, worker registry reads.
- **Worker <-> Redis**: Blocking dequeue (BRPOP), job status updates, heartbeat writes.
- **Worker <-> MinIO**: Media file download (inputs) and upload (outputs) via S3 SDK with service credentials.

---

## 3. API Design

Three separate endpoints for the three job types, each with its own validation schema. This makes each endpoint self-documenting and allows type-specific request fields.

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/jobs/overlay` | POST | Submit overlay job (requires video + subtitle filenames) |
| `/api/v1/jobs/transcode` | POST | Submit transcode job (requires video filename) |
| `/api/v1/jobs/extract` | POST | Submit extract job (requires video filename) |
| `/api/v1/jobs/:id/confirm` | POST | Confirm file upload is complete, push job to queue |
| `/api/v1/jobs/:id` | GET | Poll job status, get download URL when complete |
| `/api/v1/workers` | GET | List active workers with last-seen timestamps |
| `/healthz` | GET | Health check / liveness probe |

### Request/Response Examples

**Submit overlay job:**

```
POST /api/v1/jobs/overlay
Request:  {"input_filename": "movie.mp4", "subtitle_filename": "subs.srt"}
Response: 202 Accepted
{
  "job_id": "abc-123",
  "status": "waiting_for_upload",
  "upload_urls": {
    "video": "http://minio:9000/media-pipeline/inputs/abc-123/movie.mp4?X-Amz-Signature=...",
    "subtitle": "http://minio:9000/media-pipeline/inputs/abc-123/subs.srt?X-Amz-Signature=..."
  }
}
```

**Confirm upload:**

```
POST /api/v1/jobs/abc-123/confirm
Response: 200 OK
{"job_id": "abc-123", "status": "pending"}
```

**Poll status (completed):**

```
GET /api/v1/jobs/abc-123
Response: 200 OK
{
  "job_id": "abc-123",
  "type": "overlay",
  "status": "completed",
  "created_at": "2026-03-13T10:00:00Z",
  "completed_at": "2026-03-13T10:02:35Z",
  "download_url": "http://minio:9000/media-pipeline/outputs/abc-123/output.mp4?X-Amz-Signature=..."
}
```

**Poll status (failed):**

```
GET /api/v1/jobs/abc-123
Response: 200 OK
{
  "job_id": "abc-123",
  "type": "overlay",
  "status": "failed",
  "created_at": "2026-03-13T10:00:00Z",
  "error": "ffmpeg error: subtitle codec not supported"
}
```

---

## 4. Data Flow

### Step 1: Submit Job

User sends a POST to the appropriate job endpoint with filenames only (no files).

The Gateway:
1. Generates a UUID for the job
2. Creates a Redis hash (`job:{id}`) with metadata: type, status (`waiting_for_upload`), input keys, output key, timestamps
3. Sets a **2-hour TTL** on the Redis key via `EXPIRE` -- auto-cleanup if the user abandons the job
4. Generates **pre-signed PUT URLs** from MinIO (15-minute expiry) for the expected input files
5. Returns the job ID and upload URLs

At this point: a lightweight metadata record exists in Redis with an auto-expiry TTL. No files in MinIO. No queue push. No worker involvement.

### Step 2: Upload Files

User uploads files directly to MinIO using the pre-signed URLs. The Gateway is not in the data path -- files flow from the client straight to MinIO.

If the user never uploads: the pre-signed URL expires in 15 minutes (MinIO rejects late uploads) and the Redis key auto-deletes after 2 hours. Zero cleanup code needed.

### Step 2.5: Confirm Upload

User calls `POST /api/v1/jobs/:id/confirm`.

The Gateway:
1. Verifies the expected files exist in MinIO (HEAD request per file)
2. If files are missing: returns 400 error, job stays in `waiting_for_upload`
3. If files exist: calls `PERSIST` on the Redis key (removes the 2-hour TTL), sets status to `pending`, pushes the job ID onto the Redis queue via `LPUSH jobs:queue`

This is the gate between "user intent" and "worker work." Jobs only enter the queue when files are verified.

### Step 3: Worker Processes Job

A free worker is blocked on `BRPOP jobs:queue 0`. Redis wakes it up and delivers the job ID.

The worker:
1. Reads the job record from Redis (`HGETALL job:{id}`)
2. Validates required fields for the job type
3. If validation fails: marks job `failed` with error, returns to BRPOP immediately
4. Claims the job: sets status to `processing`, stores its worker ID
5. Starts a heartbeat goroutine (updates `last_heartbeat` every 5 seconds)
6. Downloads input files from MinIO to a local temp directory
7. If download fails: marks job `failed`, cleans up, returns to BRPOP
8. Executes the appropriate FFmpeg command as a child process
9. If FFmpeg fails (non-zero exit): marks job `failed` with stderr, cleans up, returns to BRPOP
10. Uploads the output file to MinIO
11. Marks job `completed` with the output key and timestamp
12. Stops heartbeat, deletes temp directory, returns to BRPOP for the next job

At every step, failure is handled by marking the job `failed`, cleaning up, and immediately becoming free for the next job. The worker never gets stuck.

### Step 4: Poll and Download

User polls `GET /api/v1/jobs/:id` periodically. When status is `completed`, the response includes a pre-signed GET URL for the output file. The user downloads the result directly from MinIO.

### Output by Job Type

| Job Type | FFmpeg Command | Output |
|----------|---------------|--------|
| Overlay | `ffmpeg -i input.mp4 -vf subtitles=subs.srt output.mp4` | `.mp4` with burned subtitles |
| Transcode | `ffmpeg -i input.mp4 -vf scale=-2:480 -c:a copy output.mp4` | `.mp4` downscaled to 480p |
| Extract | `ffmpeg -i input.mp4 -vn -acodec libmp3lame output.mp3` | `.mp3` audio only |

---

## 5. Worker Architecture

### Generic Workers (not type-specialized)

All workers are identical -- every worker can handle any of the three job types. The job type is stored in the Redis job record; the worker reads it and picks the right FFmpeg command.

**Why generic over specialized:**
- All three jobs use the same tool (FFmpeg) with the same resource profile (CPU + disk)
- Better resource utilization -- if 10 transcode jobs arrive and zero overlay jobs, all workers share the transcode load instead of overlay workers sitting idle
- Simpler scaling -- `docker-compose up --scale worker=N` adds N workers that handle any type
- Single queue, single worker binary, single Docker image

### Pull-Based Distribution

Workers pull jobs from the queue using `BRPOP`. Redis does not track which workers are free. Free workers are the ones currently blocked on `BRPOP`. Busy workers are running FFmpeg and not asking for work. When a job arrives, Redis delivers it to the longest-waiting worker.

No load balancer, no scheduler, no assignment logic. The queue itself is the distribution mechanism.

### Horizontal Scaling

```
docker-compose up --scale worker=N
```

New workers immediately start competing on `BRPOP`. Redis fairly distributes jobs. Workers can be added or removed at any time without configuration changes. The Gateway never holds a list of workers in memory.

---

## 6. Elastic Orchestration

### Worker Discovery

Workers register themselves in a Redis sorted set (`workers:active`) with their worker ID as the member and the current timestamp as the score. The heartbeat goroutine updates this score every 5 seconds.

The Gateway reads this sorted set to serve `GET /api/v1/workers`. It does not use this set for job assignment -- BRPOP handles that.

### Workers Joining

A new worker starts, registers in `workers:active`, and calls `BRPOP`. It immediately starts receiving jobs. No handshake with the Gateway, no configuration update.

### Workers Leaving

A worker stops (graceful shutdown, crash, or scale-down). It stops heartbeating. The Gateway's reaper goroutine scans `workers:active` for scores older than 30 seconds and removes stale entries. Any in-flight job from the dead worker is handled by the reaper (see Resiliency below).

---

## 7. Resiliency and Recovery

### Three Protection Layers

| Layer | Protects Against | Mechanism | Timeout |
|-------|-----------------|-----------|---------|
| Pre-signed URL expiry | User never starts uploading | MinIO rejects expired URLs | 15 minutes |
| Redis key TTL | Orphaned job records (user abandons after Step 1) | `EXPIRE` on job hash, auto-deleted by Redis | 2 hours |
| Reaper goroutine | Worker crash during processing | Heartbeat monitoring + automatic re-queue | 30 second heartbeat window |

### Job-Level Recovery (Reaper)

The Gateway runs a reaper goroutine every 15 seconds. It scans for jobs in `processing` state whose `last_heartbeat` is older than 30 seconds. For each stale job:

1. Increment `retry_count`
2. If `retry_count < 3`: set status back to `pending`, push back to `jobs:queue`. Another worker picks it up.
3. If `retry_count >= 3`: set status to `failed` with error `"max retries exceeded"`. Manual investigation needed.

### Worker Self-Recovery

At every step during processing, the worker handles failures by:
1. Marking the job `failed` with a descriptive error
2. Stopping the heartbeat goroutine
3. Cleaning up temp files
4. Returning to `BRPOP` -- free for the next job

The worker never gets stuck on a bad job.

### Redis Persistence

Redis is configured with AOF (`appendonly yes`). Job state and queue contents survive Redis restarts.

---

## 8. Redis Data Model

### Job Record (Hash)

Key: `job:{uuid}`

| Field | Type | Description |
|-------|------|-------------|
| type | string | `overlay`, `transcode`, or `extract` |
| status | string | `waiting_for_upload`, `pending`, `processing`, `completed`, `failed` |
| input_key | string | MinIO path for input video |
| subtitle_key | string | MinIO path for subtitle file (overlay only) |
| output_key | string | MinIO path for output file |
| worker_id | string | ID of the worker processing this job |
| last_heartbeat | int64 | Unix timestamp of last heartbeat from worker |
| retry_count | int | Number of times this job has been re-queued |
| error | string | Error message if failed |
| created_at | string | ISO 8601 timestamp |
| completed_at | string | ISO 8601 timestamp |

### Job Queue (List)

Key: `jobs:queue`

A FIFO list of job IDs. Gateway pushes via `LPUSH`, workers pop via `BRPOP`.

### Worker Registry (Sorted Set)

Key: `workers:active`

Members are worker IDs, scores are Unix timestamps of last heartbeat. Used for observability and dead worker detection, not for job assignment.

---

## 9. MinIO Storage Layout

Bucket: `media-pipeline`

```
media-pipeline/
├── inputs/
│   └── {job_id}/
│       ├── movie.mp4          (source video)
│       └── subs.srt           (subtitle file, overlay only)
└── outputs/
    └── {job_id}/
        └── output.mp4|mp3     (processed result)
```

Files are accessed via:
- **Pre-signed URLs** (client uploads/downloads) -- time-limited, no credentials needed
- **S3 SDK with service credentials** (workers download/upload) -- internal access

---

## 10. Containerization

### docker-compose.yml Services

| Service | Image | Ports | Scaling |
|---------|-------|-------|---------|
| gateway | Custom Go build | 8080 (host) | Single instance |
| worker | Custom Go build + FFmpeg | None (internal) | `--scale worker=N` |
| redis | redis:7-alpine | 6379 (internal) | Single instance |
| minio | minio/minio | 9000, 9001 (host) | Single instance |

### Worker Dockerfile

Based on Alpine with FFmpeg installed. The Go binary is compiled and copied in via multi-stage build. Workers connect to Redis and MinIO via Docker Compose service names (e.g., `redis:6379`, `minio:9000`).

---

## 11. Project Structure

```
Distributed_Media_Pipeline/
├── hld design.md              # This document
├── README.md                  # Setup, usage, operational strategy
├── docker-compose.yml
├── gateway/
│   ├── Dockerfile
│   ├── main.go                # HTTP server setup, reaper startup
│   ├── handler/               # HTTP handlers (overlay, transcode, extract, confirm, status)
│   ├── service/               # Business logic (job creation, confirmation, reaper)
│   ├── store/                 # Redis interactions
│   └── storage/               # MinIO interactions (pre-signed URL generation)
├── worker/
│   ├── Dockerfile
│   ├── main.go                # Worker startup, registration, BRPOP loop
│   ├── processor/             # FFmpeg command execution per job type
│   └── heartbeat/             # Heartbeat goroutine
├── shared/
│   └── config/                # Shared configuration (env parsing, constants)
├── go.mod
├── go.sum
└── scripts/
    └── demo.sh                # curl-based demo: submit, upload, confirm, poll, download
```

---

## 12. Architectural Trade-offs

| Decision | Choice | Alternative | Why We Chose This |
|----------|--------|-------------|-------------------|
| Language | Go | Python, Node.js | Excellent concurrency (goroutines for heartbeat, reaper), single binary deploys, strong standard library |
| Queue | Redis List (BRPOP) | Kafka, SQS, RabbitMQ | Zero extra infra (Redis already used for state), simple, proven. No built-in DLQ or visibility timeout -- we implement via heartbeat + reaper. |
| State Store | Redis Hash | PostgreSQL, DynamoDB | Co-located with queue, sub-millisecond reads. Not durable long-term; for production we'd add a relational DB. |
| Object Store | MinIO | Local volume mounts, NFS | S3-compatible (production-portable), pre-signed URLs decouple Gateway from file transfer. Extra container, but avoids Gateway becoming a bottleneck. |
| File Transfer | Pre-signed URLs | Multipart upload through Gateway | Gateway stays lightweight, never proxies media bytes. Extra client step (confirm), but demo script automates it. |
| Upload Confirmation | Explicit `/confirm` endpoint | MinIO bucket notifications, Gateway polling | Simplest, no extra infra config. Client knows when its upload finished. Gateway verifies files on confirm. |
| Abandoned Job Cleanup | Redis TTL (EXPIRE) + pre-signed URL expiry | Reaper-based scan for stale uploads | Native Redis mechanism, zero code for the common case. Exact timing, no race conditions. |
| Worker Model | Generic (any worker handles any type) | Specialized (one worker per type) | All jobs use FFmpeg with same resource profile. Better utilization, simpler scaling, single queue. |
| Job Distribution | Pull-based (BRPOP) | Push-based (Gateway assigns to workers) | No scheduler needed. Free workers self-select. Redis handles fair distribution natively. |
| Orchestration | Docker Compose | Kubernetes | Meets the requirement, easy local dev. Not production-grade; K8s with HPA for production. |
| Worker Discovery | Redis Sorted Set + heartbeat | Consul, etcd, DNS-based | No service mesh dependency, simple. Slight delay in detecting dead workers (heartbeat window). |
| Crash Recovery | Heartbeat + reaper + retry counter | Kafka consumer group rebalancing | Works with Redis List queue. Manual implementation, but gives full control over retry policy. |

---

## 13. Job State Machine

```
waiting_for_upload  -->  pending  -->  processing  -->  completed
        |                  |              |
        v                  v              v
   (expired/TTL)       (reaper re-queues on crash, up to 3x)
        |                                 |
        v                                 v
      (deleted)                         failed
```

- `waiting_for_upload`: Job created, waiting for client to upload files and confirm. Redis TTL active (2h).
- `pending`: Files confirmed, job in queue, waiting for a worker. TTL removed.
- `processing`: Worker claimed the job, FFmpeg running. Heartbeat active.
- `completed`: Output uploaded to MinIO, download URL available.
- `failed`: FFmpeg error, download failure, max retries exceeded, or upload timeout.
