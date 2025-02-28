#!/bin/sh

CONFIG_FILE="/app/config.yaml"
SYNC_DIR="/app/sync"

# Ensure the sync directory exists
mkdir -p "$SYNC_DIR"

# Check if config.yaml exists, if not, initialize it
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Initializing gorsync..."
    ./gorsync init
fi

# Start gorsync
exec ./gorsync start
