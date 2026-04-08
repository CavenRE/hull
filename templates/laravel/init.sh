#!/bin/bash
TARGET_DIR=$1
SITE_NAME=$2

echo "Bootstrapping fresh Laravel installation into $SITE_NAME..."

docker run --rm \
    --user $(id -u):$(id -g) \
    -v "$TARGET_DIR:/app" \
    -w /app \
    composer:latest \
    sh -c "composer create-project laravel/laravel tmp && cp -a tmp/. . && rm -rf tmp"

echo "Laravel files downloaded successfully."
