#!/bin/bash

# Simple test script for Corkscrew gRPC API
# This script starts the server and tests basic functionality

PORT=9092
HOST=localhost

echo "🚀 Testing Corkscrew gRPC API..."
echo

# Start server in background
echo "📡 Starting Corkscrew API server on port $PORT..."
./corkscrew serve --port $PORT --host $HOST &
SERVER_PID=$!

# Wait for server to start
echo "⏳ Waiting for server to start..."
sleep 3

echo
echo "🧪 Running API tests..."
echo

# Test 1: List available services
echo "1️⃣ Testing service listing..."
if command -v grpcurl >/dev/null 2>&1; then
    grpcurl -plaintext $HOST:$PORT list
else
    echo "❌ grpcurl not found - install with: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest"
fi

echo
echo "2️⃣ Testing health check..."
if command -v grpcurl >/dev/null 2>&1; then
    grpcurl -plaintext $HOST:$PORT corkscrew.api.CorkscrewAPI.HealthCheck
else
    echo "❌ grpcurl not found"
fi

echo
echo "3️⃣ Testing provider listing..."
if command -v grpcurl >/dev/null 2>&1; then
    grpcurl -plaintext $HOST:$PORT corkscrew.api.CorkscrewAPI.ListProviders
else
    echo "❌ grpcurl not found"
fi

echo
echo "4️⃣ Testing provider status..."
if command -v grpcurl >/dev/null 2>&1; then
    grpcurl -plaintext -d '{"include_status": true}' $HOST:$PORT corkscrew.api.CorkscrewAPI.ListProviders
else
    echo "❌ grpcurl not found"
fi

# Clean up
echo
echo "🛑 Stopping server..."
kill $SERVER_PID 2>/dev/null
wait $SERVER_PID 2>/dev/null

echo
echo "✅ API testing completed!"
echo
echo "💡 To test the API manually:"
echo "   1. Start server: ./corkscrew serve --port $PORT"
echo "   2. Install grpcurl: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest"
echo "   3. Test commands:"
echo "      grpcurl -plaintext $HOST:$PORT list"
echo "      grpcurl -plaintext $HOST:$PORT corkscrew.api.CorkscrewAPI.HealthCheck"
echo "      grpcurl -plaintext $HOST:$PORT corkscrew.api.CorkscrewAPI.ListProviders"
echo
echo "📖 For more examples, see the server output when you run 'corkscrew serve'"