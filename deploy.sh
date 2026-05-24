#!/bin/bash
# One-click deployment script
set -e

echo "=== Nginx Request Attribution - One-Click Deploy ==="

# Check if Go is installed
if command -v go &> /dev/null; then
    echo "✅ Go found, building from source..."
    go build -ldflags="-s -w" -o nginx-req-attr ./cmd/
    echo "✅ Built successfully: ./nginx-req-attr"
elif command -v docker &> /dev/null; then
    echo "✅ Docker found, building container..."
    docker build -t nginx-req-attr .
    echo "✅ Docker image built: nginx-req-attr"
    echo ""
    echo "Run with:"
    echo "  docker run -d -p 8080:8080 -v /var/log/nginx:/var/log/nginx:ro -v ./data:/app/data nginx-req-attr"
    exit 0
else
    echo "❌ Neither Go nor Docker found. Please install one of them."
    exit 1
fi

# Create default config if not exists
if [ ! -f config.json ]; then
    cp config.example.json config.json
    echo "✅ Created config.json from example"
fi

# Create data directory
mkdir -p data

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Usage:"
echo "  # Import existing logs"
echo "  ./nginx-req-attr -import /var/log/nginx/access.log"
echo ""
echo "  # Start server (watch logs + web GUI)"
echo "  ./nginx-req-attr -config config.json"
echo ""
echo "  # Open dashboard at http://localhost:8080"
echo ""
