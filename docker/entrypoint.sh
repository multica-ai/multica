#!/bin/sh
set -e

echo "Cloning ai-enhancement-hub skills..."
if [ ! -d "$HOME/ai-enhancement-hub" ]; then
  git clone --depth=1 https://github.com/g2crowd/ai-enhancement-hub "$HOME/ai-enhancement-hub"
else
  git -C "$HOME/ai-enhancement-hub" pull --ff-only
fi
mkdir -p "$HOME/.agents/skills"
cp -r "$HOME/ai-enhancement-hub/skills/." "$HOME/.agents/skills/"

echo "Running database migrations..."
./migrate up

echo "Starting server..."
exec ./server
