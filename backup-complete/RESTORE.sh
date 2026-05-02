#!/bin/bash
# Multica Complete Restore Script
set -e
echo "=== Multica Complete Restore ==="

# Check prerequisites
command -v docker >/dev/null 2>&1 || { echo "Docker required"; exit 1; }
command -v node >/dev/null 2>&1 || { echo "Node.js required"; exit 1; }

# 1. Clone the repo
REPO=${1:-"https://github.com/AnwarPy/multica-complete.git"}
echo "Cloning from $REPO..."
git clone "$REPO" multica-complete
cd multica-complete

# 2. Checkout your branch
git checkout fix/daemon-api-timeout-heartbeat

# 3. Install dependencies
echo "Installing dependencies..."
npm install

# 4. Setup env
cp deployment/.env.example deployment/.env 2>/dev/null || true
echo "Edit deployment/.env with your credentials"

# 5. Build Docker image
echo "Building Docker image..."
docker build -f Dockerfile.web.rtl -t multica-web-rtl:latest .

# 6. Create network if needed
docker network create multica-net 2>/dev/null || true

# 7. Run containers
docker run -d --name multica-frontend -p 3000:3000 --network multica-net multica-web-rtl:latest
echo "Frontend running on http://localhost:3000"

# 8. Setup memory pipeline (optional)
if [ -d "pipeline" ]; then
    cp pipeline/*.py ~/.hermes/scripts/ 2>/dev/null || true
    echo "Memory pipeline scripts copied to ~/.hermes/scripts/"
fi

echo "=== Restore Complete ==="
echo "Edit deployment/.env before running backend"
