#!/bin/bash
set -euo pipefail

TARGET_DIR=$1
SITE_NAME=$2

echo "Preparing WordPress environment for $SITE_NAME..."
echo "Note: WordPress core files will be automatically populated by the container upon boot."