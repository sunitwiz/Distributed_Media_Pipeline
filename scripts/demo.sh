#!/bin/bash
set -e

GATEWAY_URL="http://localhost:8080"
SAMPLE_VIDEO="${1:-sample.mp4}"
SAMPLE_SRT="${2:-sample.srt}"

if [ ! -f "$SAMPLE_VIDEO" ]; then
  echo "generating a 5-second test video..."
  ffmpeg -y -f lavfi -i testsrc=duration=5:size=1280x720:rate=30 \
    -f lavfi -i sine=frequency=440:duration=5 \
    -c:v libx264 -c:a aac -shortest "$SAMPLE_VIDEO" 2>/dev/null
fi

if [ ! -f "$SAMPLE_SRT" ]; then
  echo "generating a test subtitle file..."
  cat > "$SAMPLE_SRT" <<SRTEOF
1
00:00:00,000 --> 00:00:02,000
Hello RenderMesh

2
00:00:02,000 --> 00:00:05,000
Distributed Media Pipeline Demo
SRTEOF
fi

echo ""
echo "=== DEMO: Transcode Job (downscale to 480p) ==="
echo ""

echo "step 1: submitting transcode job..."
RESPONSE=$(curl -s -X POST "$GATEWAY_URL/api/v1/jobs/transcode" \
  -H "Content-Type: application/json" \
  -d "{\"input_filename\": \"$(basename $SAMPLE_VIDEO)\"}")
echo "response: $RESPONSE"

JOB_ID=$(echo "$RESPONSE" | grep -o '"job_id":"[^"]*"' | cut -d'"' -f4)
VIDEO_URL=$(echo "$RESPONSE" | grep -o '"video":"[^"]*"' | cut -d'"' -f4)

echo ""
echo "step 2: uploading video file..."
curl -s -X PUT "$VIDEO_URL" --upload-file "$SAMPLE_VIDEO" > /dev/null
echo "upload complete"

echo ""
echo "step 2.5: confirming upload..."
CONFIRM=$(curl -s -X POST "$GATEWAY_URL/api/v1/jobs/$JOB_ID/confirm")
echo "response: $CONFIRM"

echo ""
echo "step 3-4: polling for completion..."
while true; do
  STATUS_RESP=$(curl -s "$GATEWAY_URL/api/v1/jobs/$JOB_ID")
  STATUS=$(echo "$STATUS_RESP" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
  echo "  status: $STATUS"
  if [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 3
done

echo ""
echo "final response: $STATUS_RESP"

if [ "$STATUS" = "completed" ]; then
  DOWNLOAD_URL=$(echo "$STATUS_RESP" | grep -o '"download_url":"[^"]*"' | cut -d'"' -f4)
  echo ""
  echo "step 5: downloading result..."
  curl -s -o "transcode_output.mp4" "$DOWNLOAD_URL"
  echo "saved to transcode_output.mp4"
  ls -lh transcode_output.mp4
fi

echo ""
echo "=== DEMO: Extract Job (audio to mp3) ==="
echo ""

echo "step 1: submitting extract job..."
RESPONSE=$(curl -s -X POST "$GATEWAY_URL/api/v1/jobs/extract" \
  -H "Content-Type: application/json" \
  -d "{\"input_filename\": \"$(basename $SAMPLE_VIDEO)\"}")
echo "response: $RESPONSE"

JOB_ID=$(echo "$RESPONSE" | grep -o '"job_id":"[^"]*"' | cut -d'"' -f4)
VIDEO_URL=$(echo "$RESPONSE" | grep -o '"video":"[^"]*"' | cut -d'"' -f4)

echo ""
echo "step 2: uploading video file..."
curl -s -X PUT "$VIDEO_URL" --upload-file "$SAMPLE_VIDEO" > /dev/null
echo "upload complete"

echo ""
echo "step 2.5: confirming upload..."
CONFIRM=$(curl -s -X POST "$GATEWAY_URL/api/v1/jobs/$JOB_ID/confirm")
echo "response: $CONFIRM"

echo ""
echo "step 3-4: polling for completion..."
while true; do
  STATUS_RESP=$(curl -s "$GATEWAY_URL/api/v1/jobs/$JOB_ID")
  STATUS=$(echo "$STATUS_RESP" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
  echo "  status: $STATUS"
  if [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 3
done

echo ""
echo "final response: $STATUS_RESP"

if [ "$STATUS" = "completed" ]; then
  DOWNLOAD_URL=$(echo "$STATUS_RESP" | grep -o '"download_url":"[^"]*"' | cut -d'"' -f4)
  echo ""
  echo "step 5: downloading result..."
  curl -s -o "extract_output.mp3" "$DOWNLOAD_URL"
  echo "saved to extract_output.mp3"
  ls -lh extract_output.mp3
fi

echo ""
echo "=== DEMO: Overlay Job (burn subtitles) ==="
echo ""

echo "step 1: submitting overlay job..."
RESPONSE=$(curl -s -X POST "$GATEWAY_URL/api/v1/jobs/overlay" \
  -H "Content-Type: application/json" \
  -d "{\"input_filename\": \"$(basename $SAMPLE_VIDEO)\", \"subtitle_filename\": \"$(basename $SAMPLE_SRT)\"}")
echo "response: $RESPONSE"

JOB_ID=$(echo "$RESPONSE" | grep -o '"job_id":"[^"]*"' | cut -d'"' -f4)
VIDEO_URL=$(echo "$RESPONSE" | grep -o '"video":"[^"]*"' | cut -d'"' -f4)
SUBTITLE_URL=$(echo "$RESPONSE" | grep -o '"subtitle":"[^"]*"' | cut -d'"' -f4)

echo ""
echo "step 2: uploading video and subtitle files..."
curl -s -X PUT "$VIDEO_URL" --upload-file "$SAMPLE_VIDEO" > /dev/null
curl -s -X PUT "$SUBTITLE_URL" --upload-file "$SAMPLE_SRT" > /dev/null
echo "upload complete"

echo ""
echo "step 2.5: confirming upload..."
CONFIRM=$(curl -s -X POST "$GATEWAY_URL/api/v1/jobs/$JOB_ID/confirm")
echo "response: $CONFIRM"

echo ""
echo "step 3-4: polling for completion..."
while true; do
  STATUS_RESP=$(curl -s "$GATEWAY_URL/api/v1/jobs/$JOB_ID")
  STATUS=$(echo "$STATUS_RESP" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
  echo "  status: $STATUS"
  if [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 3
done

echo ""
echo "final response: $STATUS_RESP"

if [ "$STATUS" = "completed" ]; then
  DOWNLOAD_URL=$(echo "$STATUS_RESP" | grep -o '"download_url":"[^"]*"' | cut -d'"' -f4)
  echo ""
  echo "step 5: downloading result..."
  curl -s -o "overlay_output.mp4" "$DOWNLOAD_URL"
  echo "saved to overlay_output.mp4"
  ls -lh overlay_output.mp4
fi

echo ""
echo "=== checking active workers ==="
curl -s "$GATEWAY_URL/api/v1/workers" | python3 -m json.tool 2>/dev/null || curl -s "$GATEWAY_URL/api/v1/workers"

echo ""
echo "=== demo complete ==="
