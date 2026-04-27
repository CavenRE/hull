#!/bin/bash
set -euo pipefail

TARGET_DIR=$1
SITE_NAME=$2
VERSION=${3:-}

if [ -n "$VERSION" ] && [ "$VERSION" != "latest" ]; then
    COMPOSER_TARGET="laravel/laravel=^${VERSION}"
    echo "Bootstrapping Laravel ${VERSION} into $SITE_NAME..."
else
    COMPOSER_TARGET="laravel/laravel"
    echo "Bootstrapping fresh Laravel installation into $SITE_NAME..."
fi

docker run --rm \
    --user "$(id -u):$(id -g)" \
    -v "$TARGET_DIR:/app" \
    -w /app \
    composer:latest \
    sh -c "composer create-project $COMPOSER_TARGET tmp && cp -a tmp/. . && rm -rf tmp"

echo "Applying explicit permissions for container web user (www-data)..."
docker run --rm \
    -v "$TARGET_DIR:/app" \
    -w /app \
    alpine sh -c "chown -R 33:33 storage bootstrap/cache"

echo "Laravel files downloaded and permissions secured successfully."