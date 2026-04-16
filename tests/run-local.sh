#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
# Knox Local Test Runner
# ══════════════════════════════════════════════════════════════════════════════
# Run this on an Ubuntu VM to simulate the full CI test pipeline locally.
#
# Usage:
#   chmod +x tests/run-local.sh
#   ./tests/run-local.sh              # Run all tests (n8n default version)
#   ./tests/run-local.sh 2.13.3       # Run all tests with specific n8n version
#   ./tests/run-local.sh latest unit  # Run only unit tests
#   ./tests/run-local.sh 2.13.3 e2e   # Run only E2E tests
#
# Arguments:
#   $1 — n8n version (default: 2.13.3)
#   $2 — test scope: all | unit | integration | e2e (default: all)
#
# Prerequisites (auto-installed if missing):
#   - Go 1.22+
#   - Docker + Docker Compose v2
#   - Node.js 20+ (for E2E only)
#   - curl, jq
# ══════════════════════════════════════════════════════════════════════════════
set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
N8N_VERSION="${1:-2.13.3}"
TEST_SCOPE="${2:-all}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export N8N_VERSION

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Result tracking
UNIT_RESULT="skipped"
INTEGRATION_RESULT="skipped"
E2E_RESULT="skipped"
START_TIME=$(date +%s)

# ── Helpers ───────────────────────────────────────────────────────────────────

banner() {
  echo ""
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}  $1${NC}"
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo ""
}

info()    { echo -e "${BLUE}ℹ${NC} $1"; }
success() { echo -e "${GREEN}✓${NC} $1"; }
warn()    { echo -e "${YELLOW}⚠${NC} $1"; }
fail()    { echo -e "${RED}✗${NC} $1"; }
step()    { echo -e "${CYAN}→${NC} $1"; }

cleanup() {
  local exit_code=$?
  banner "Cleanup"

  if [ "$TEST_SCOPE" = "all" ] || [ "$TEST_SCOPE" = "integration" ] || [ "$TEST_SCOPE" = "e2e" ]; then
    step "Stopping Docker containers..."
    cd "$SCRIPT_DIR"
    docker compose -f docker-compose.test.yaml down -v --remove-orphans 2>/dev/null || true
    success "Containers stopped"
  fi

  # Remove temp files
  rm -f "$SCRIPT_DIR/.test-env" 2>/dev/null || true

  # Summary
  local END_TIME=$(date +%s)
  local DURATION=$((END_TIME - START_TIME))

  banner "Test Results Summary"
  echo -e "  n8n Version:    ${BOLD}${N8N_VERSION}${NC}"
  echo -e "  Test Scope:     ${BOLD}${TEST_SCOPE}${NC}"
  echo -e "  Duration:       ${BOLD}${DURATION}s${NC}"
  echo ""

  print_result() {
    local name="$1"
    local result="$2"
    case "$result" in
      passed)  echo -e "  ${name}: ${GREEN}${BOLD}PASSED${NC}" ;;
      failed)  echo -e "  ${name}: ${RED}${BOLD}FAILED${NC}" ;;
      skipped) echo -e "  ${name}: ${YELLOW}SKIPPED${NC}" ;;
    esac
  }

  print_result "🧪 Unit Tests      " "$UNIT_RESULT"
  print_result "🔗 Integration Tests" "$INTEGRATION_RESULT"
  print_result "🌐 E2E Browser Tests" "$E2E_RESULT"
  echo ""

  if [ "$UNIT_RESULT" = "failed" ] || [ "$INTEGRATION_RESULT" = "failed" ] || [ "$E2E_RESULT" = "failed" ]; then
    fail "Some tests FAILED!"
    exit 1
  elif [ "$exit_code" -ne 0 ]; then
    fail "Script exited with error code: $exit_code"
    exit $exit_code
  else
    success "All executed tests PASSED!"
  fi
}
trap cleanup EXIT

# ══════════════════════════════════════════════════════════════════════════════
# Phase 0: Prerequisites Check
# ══════════════════════════════════════════════════════════════════════════════

banner "Phase 0 — Prerequisites Check"

# ── Check OS ──────────────────────────────────────────────────────────────────
if [ ! -f /etc/os-release ]; then
  fail "This script is designed for Ubuntu/Debian. /etc/os-release not found."
  exit 1
fi
source /etc/os-release
info "OS: ${PRETTY_NAME}"

# ── Check/Install Docker ─────────────────────────────────────────────────────
if ! command -v docker &>/dev/null; then
  warn "Docker not found. Installing..."
  sudo apt-get update -qq
  sudo apt-get install -y -qq ca-certificates curl gnupg
  sudo install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  sudo chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
  sudo apt-get update -qq
  sudo apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  sudo usermod -aG docker "$USER"
  warn "Docker installed. You may need to logout/login for group changes to take effect."
  warn "If docker commands fail, run: newgrp docker"
fi
success "Docker: $(docker --version)"

# ── Check Docker Compose v2 ──────────────────────────────────────────────────
if ! docker compose version &>/dev/null; then
  fail "Docker Compose v2 not found. Install docker-compose-plugin."
  exit 1
fi
success "Docker Compose: $(docker compose version --short)"

# ── Check/Install Go ─────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  warn "Go not found. Installing Go 1.24.2..."
  GO_VERSION="1.24.2"
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf /tmp/go.tar.gz
  rm /tmp/go.tar.gz
  export PATH="/usr/local/go/bin:$PATH"
  echo 'export PATH="/usr/local/go/bin:$PATH"' >> ~/.bashrc
fi
success "Go: $(go version)"

# ── Check/Install Node.js (for E2E) ──────────────────────────────────────────
if [ "$TEST_SCOPE" = "all" ] || [ "$TEST_SCOPE" = "e2e" ]; then
  if ! command -v node &>/dev/null || [ "$(node -v | cut -d'.' -f1 | tr -d 'v')" -lt 18 ]; then
    warn "Node.js 18+ not found. Installing Node.js 20..."
    curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
    sudo apt-get install -y -qq nodejs
  fi
  success "Node.js: $(node --version)"
  success "npm: $(npm --version)"
fi

# ── Check utilities ───────────────────────────────────────────────────────────
for cmd in curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    warn "$cmd not found. Installing..."
    sudo apt-get install -y -qq "$cmd"
  fi
done
success "curl + jq installed"

# Ensure we run from the project root
cd "$(dirname "$0")/.."

if [[ "$OSTYPE" == "darwin"* ]]; then
  export HOST_IP=$(ipconfig getifaddr en0 2>/dev/null || echo "127.0.0.1")
else
  export HOST_IP=$(hostname -I | awk '{print $1}')
  [ -z "$HOST_IP" ] && export HOST_IP="127.0.0.1"
fi

info "n8n version: $N8N_VERSION"
info "Test scope: $TEST_SCOPE"

# ══════════════════════════════════════════════════════════════════════════════
# Phase 1: Unit Tests
# ══════════════════════════════════════════════════════════════════════════════

if [ "$TEST_SCOPE" = "all" ] || [ "$TEST_SCOPE" = "unit" ]; then
  banner "Phase 1 — Go Unit Tests"

  cd "$PROJECT_ROOT/knox-proxy"

  step "Downloading Go dependencies..."
  go mod download

  step "Running go vet..."
  go vet ./...

  step "Running unit tests with race detector..."
  if go test -v -race -count=1 -cover -timeout 120s ./...; then
    UNIT_RESULT="passed"
    success "All unit tests PASSED"
  else
    UNIT_RESULT="failed"
    fail "Unit tests FAILED"
    if [ "$TEST_SCOPE" = "unit" ]; then
      exit 1
    fi
  fi

  cd "$PROJECT_ROOT"
fi

# ══════════════════════════════════════════════════════════════════════════════
# Phase 2: Start Docker Test Environment
# ══════════════════════════════════════════════════════════════════════════════

if [ "$TEST_SCOPE" = "all" ] || [ "$TEST_SCOPE" = "integration" ] || [ "$TEST_SCOPE" = "e2e" ]; then
  banner "Phase 2 — Docker Test Environment"

  cd "$SCRIPT_DIR"

  step "Building and starting test stack (n8n ${N8N_VERSION})..."
  docker compose -f docker-compose.test.yaml up -d --build

  step "Container status:"
  docker compose -f docker-compose.test.yaml ps

  # ── Wait for services ────────────────────────────────────────────────────
  step "Waiting for all services to be ready..."
  bash scripts/wait-for-services.sh

  # ── Setup Vault ──────────────────────────────────────────────────────────
  step "Seeding Vault with test data..."
  bash scripts/setup-vault.sh

  # ── Setup n8n ────────────────────────────────────────────────────────────
  step "Creating n8n test accounts and workflows..."
  bash scripts/setup-n8n-accounts.sh

  # ── Load test env ────────────────────────────────────────────────────────
  if [ -f "$SCRIPT_DIR/.test-env" ]; then
    step "Loading test environment variables..."
    set -a
    source "$SCRIPT_DIR/.test-env"
    set +a
    success "Test env loaded"
    cat "$SCRIPT_DIR/.test-env"
  fi

  success "Test environment is ready!"
fi

# ══════════════════════════════════════════════════════════════════════════════
# Phase 3: Integration Tests
# ══════════════════════════════════════════════════════════════════════════════

if [ "$TEST_SCOPE" = "all" ] || [ "$TEST_SCOPE" = "integration" ]; then
  banner "Phase 3 — Integration Tests"

  cd "$SCRIPT_DIR/integration"

  step "Running integration tests (timeout: 300s)..."
  if go test -v -timeout 300s -count=1 ./...; then
    INTEGRATION_RESULT="passed"
    success "All integration tests PASSED"
  else
    INTEGRATION_RESULT="failed"
    fail "Integration tests FAILED"

    # Dump logs for debugging
    step "Dumping container logs for debugging..."
    cd "$SCRIPT_DIR"
    docker compose -f docker-compose.test.yaml logs --no-color > test-logs.txt
    warn "Logs saved to: $SCRIPT_DIR/test-logs.txt"

    if [ "$TEST_SCOPE" = "integration" ]; then
      exit 1
    fi
  fi

  cd "$PROJECT_ROOT"
fi

# ══════════════════════════════════════════════════════════════════════════════
# Phase 4: E2E Browser Tests
# ══════════════════════════════════════════════════════════════════════════════

if [ "$TEST_SCOPE" = "all" ] || [ "$TEST_SCOPE" = "e2e" ]; then
  banner "Phase 4 — E2E Browser Tests (Playwright)"

  cd "$SCRIPT_DIR/e2e"

  step "Installing npm dependencies..."
  npm ci

  step "Installing Playwright browsers..."
  npx playwright install --with-deps chromium

  step "Running E2E tests..."
  if npx playwright test; then
    E2E_RESULT="passed"
    success "All E2E tests PASSED"
  else
    E2E_RESULT="failed"
    fail "E2E tests FAILED"

    # Dump logs for debugging
    step "Dumping container logs for debugging..."
    cd "$SCRIPT_DIR"
    docker compose -f docker-compose.test.yaml logs --no-color > test-logs.txt
    warn "Logs saved to: $SCRIPT_DIR/test-logs.txt"

    warn "Playwright report saved to: $SCRIPT_DIR/e2e/playwright-report/"
    warn "View report: npx playwright show-report $SCRIPT_DIR/e2e/playwright-report/"
  fi

  cd "$PROJECT_ROOT"
fi

# Cleanup runs via trap
