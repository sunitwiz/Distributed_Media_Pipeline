# Distributed_Media_Pipeline
RenderMesh is a distributed media processing pipeline that orchestrates containerized worker nodes to run FFmpeg jobs such as subtitle overlay, video transcoding, and audio extraction. It provides an API gateway for job submission, fault-tolerant processing, and scalable worker coordination using Docker Compose.

---

## Observability and Alerting

The system already has a heartbeat mechanism -- workers update a timestamp in Redis every 5 seconds while processing a job. This is the foundation for detecting problems.

A "zombied" worker is one where the container is running but the process is stuck (FFmpeg hung, deadlock, etc.). We detect this because the heartbeat stops updating. The reaper goroutine catches it within 30 seconds and re-queues the job. But for real-time visibility beyond the reaper, we'd add:

Prometheus metrics exposed by both the Gateway and Workers:
- jobs_in_queue -- how many jobs are waiting to be picked up
- jobs_processing_duration_seconds -- how long each job has been running
- worker_heartbeat_age_seconds -- time since a worker last checked in
- jobs_total by status -- counters for completed, failed, retried jobs
- reaper_requeued_total -- how often the reaper is rescuing stuck jobs

Grafana dashboards built on top of these metrics to show queue depth over time, active worker count, average processing time, and failure rates.

Alerts we'd configure:
- Job stuck in "processing" for more than 30 minutes (likely zombied worker or extremely large file)
- Zero active workers (nobody is consuming from the queue)
- Queue depth growing for more than 10 minutes (workers can't keep up)
- Job failure rate exceeds 20% in a 15 minute window (something systemic is wrong, bad FFmpeg version, MinIO issues)
- Reaper re-queue count spikes (workers are crashing repeatedly)
- Jobs sitting in "pending" for more than 5 minutes with no worker picking them up

For a stuck job that's not in "processing" -- say it's been in "waiting_for_upload" past the 2 hour TTL -- Redis handles that automatically via key expiry. No alert needed, it just cleans itself up.









---

## Scale: From 10 Jobs/Hour to 10,000 Jobs/Hour

At 10 jobs/hour, our current setup (Redis list, single Gateway, MinIO, a few workers in Docker Compose) is perfectly fine. At 10,000 jobs/hour, several parts of the system would need to change.

Queue: Redis List with BRPOP works great at low volume, but at 10k/hour you need partitioned consumption, built-in acknowledgment, and replay capability. Switch to Kafka with consumer groups, or AWS SQS. Each partition can be consumed by a different set of workers independently.

State store: Redis hashes are fast but not ideal for querying thousands of job records, running analytics, or long-term retention. Move job state to PostgreSQL. Keep Redis as a cache layer for hot reads (active job status polling) and for the queue if staying with Redis Streams instead of Kafka.

Object storage: MinIO works in Docker but isn't built for heavy production load. Replace with AWS S3 or a distributed MinIO cluster with erasure coding across multiple nodes.

Gateway: Already stateless (all state is in Redis/Postgres), so scaling it is straightforward. Put multiple Gateway instances behind a load balancer. No code changes needed.

Workers: This is where the real scaling happens. Move from Docker Compose to Kubernetes. Use Horizontal Pod Autoscaler (HPA) with a custom metric -- queue depth. When the queue grows, K8s spins up more worker pods. When it shrinks, it scales down. You could also add priority queues (urgent jobs get processed first) and resource-based routing (heavy transcode jobs go to workers with more CPU/memory).

Monitoring: At this scale, the Prometheus + Grafana setup becomes essential rather than optional. You'd also want distributed tracing (OpenTelemetry) to follow a job through the Gateway, queue, worker, and back.

The core architecture (Gateway submits, queue distributes, workers pull and process) stays the same. What changes is the specific technology behind each component to handle the throughput and durability requirements.



















---

## Agentic Orchestration

Right now, users submit explicit jobs: "overlay this", "transcode that". Each is a single atomic operation. But what if the user says something like "trim this video from 10-30s and then burn subtitles on top of the trimmed output"?

This requires two things the current system doesn't have: understanding what the user wants, and chaining multiple jobs together.

The approach would be to add an LLM-powered planner layer in the Gateway. When a user hits a new endpoint like `POST /api/v1/pipelines` with a free-text prompt, the planner parses that text into a DAG (directed acyclic graph) of atomic job steps.

For the example above, the planner would produce:
- Step 1: Trim video (10s to 30s) -- this would be a new job type we'd add to the worker
- Step 2: Overlay subtitles onto the trimmed output -- existing job type

Step 2 depends on Step 1's output. The system creates both jobs, but only queues Step 1. When Step 1 completes, a new component (the DAG executor) detects the completion, wires Step 1's output as Step 2's input, and queues Step 2. The user gets a pipeline ID and polls that for overall status.

For cases where steps are independent (e.g., "extract audio AND transcode to 480p"), the executor runs them in parallel since neither depends on the other.

The key insight is that the worker pool stays exactly the same. Workers still pull atomic jobs from the queue and run FFmpeg. The agentic layer sits above them in the Gateway and only decides which jobs to create and in what order. It's an orchestration concern, not a processing concern.

To add new capabilities (trim, merge, speed change, etc.), you'd add new job types to the worker's FFmpeg command map and register them with the planner so the LLM knows what tools are available. The LLM essentially acts as a function-calling agent where the available functions are the worker job types.

The pipeline API would look something like:

```
POST /api/v1/pipelines
{"prompt": "trim from 10-30s then burn subtitles", "input_filename": "movie.mp4", "subtitle_filename": "subs.srt"}

Response:
{"pipeline_id": "pipe-xyz", "steps": [{"type": "trim", "status": "pending"}, {"type": "overlay", "status": "waiting"}]}
```

And polling would show the progress of each step in the chain.
