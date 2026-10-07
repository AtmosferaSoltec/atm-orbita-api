#!/bin/sh
set -e

if [ -n "$DATABASE_URL" ]; then
  echo "Running Prisma migrations..."
  ./node_modules/.bin/prisma migrate deploy
fi

exec "$@"
