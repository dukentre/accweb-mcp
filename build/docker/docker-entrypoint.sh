#!/bin/sh
set -eu

command=${1:-start}

if [ "$command" = "start" ]; then
    envsubst < docker_config.yml > config.yml
    exec ./accweb
else
    exec "$@"
fi
