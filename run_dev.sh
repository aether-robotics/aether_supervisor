#!/bin/bash

docker run -d --name "supervisor" \
  --tty \
  --privileged \
  --restart "always" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_CLEANUP=true \
  -e WATCHTOWER_INCLUDE_RESTARTING=true \
  -e WATCHTOWER_HTTP_API_TOKEN=robotics \
  -e WATCHTOWER_HTTP_API_PERIODIC_POLLS=true \
  -p 8080:8080 \
  --label=com.centurylinklabs.watchtower.enable=false \
  dkhoanguyen/aether_supervisor:latest \
  --interval 3000 \
  --http-api-update \
  --http-api-port 8080 \
  --update-on-start \
  --monitor-only