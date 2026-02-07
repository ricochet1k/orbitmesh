#!/bin/bash
set -e

echo "OrbitMesh Development Environment Setup"
echo "========================================"
echo ""

# Check prerequisites
if ! command -v go &> /dev/null; then
    echo "Error: Go is required but not installed"
    echo "Visit: https://golang.org/doc/install"
    exit 1
fi

if ! command -v node &> /dev/null; then
    echo "Error: Node.js is required but not installed"
    echo "Visit: https://nodejs.org/"
    exit 1
fi

echo "✓ Go version: $(go version)"
echo "✓ Node version: $(node --version)"
echo ""

# Install Taskfile if not present
if ! command -v task &> /dev/null; then
    echo "Installing Taskfile..."
    go install github.com/go-task/task/v3/cmd/task@latest
    echo "✓ Taskfile installed"
else
    echo "✓ Taskfile already installed: $(task --version)"
fi
echo ""

# Run task setup
echo "Running setup tasks..."
task setup

echo ""
echo "✅ Setup complete!"
echo ""
echo "Next steps:"
if command -v overmind &> /dev/null && command -v tmux &> /dev/null; then
    echo "  1. Run 'task dev' to start the development environment"
else
    echo "  1. Install Overmind + tmux (macOS: 'brew install overmind tmux')"
    echo "     - or run 'task dev:manual' for manual commands"
fi
echo "  2. Backend will be available at http://localhost:8080"
echo "  3. Frontend will be available at http://localhost:3000"
echo ""
echo "For more commands, run 'task --list'"
