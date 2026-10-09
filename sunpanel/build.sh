#!/bin/sh
set -eu
cd "$(dirname "$0")"
npx --yes pnpm@8.15.9 install --frozen-lockfile
npx --yes pnpm@8.15.9 run build-only
mkdir -p service/integration/web
rm -rf service/integration/web/*
cp -R dist/. service/integration/web/
