#!/bin/bash

docker build --no-cache --tag lkschu/doctray . --network=host
docker push lkschu/doctray:latest
docker tag lkschu/doctray:latest lkschu/doctray:$(date --iso-8601)
docker push lkschu/doctray:$(date --iso-8601)
