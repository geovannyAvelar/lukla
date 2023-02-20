#!/bin/bash

docker build -t lukla .
docker stop lukla
docker container rm lukla
docker run --name lukla -d -p 9000:9000 lukla