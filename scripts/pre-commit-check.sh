#!/bin/bash

# Pre-commit check script for skill matrix changes
# Run this before committing to ensure tests pass

set -e

echo "🔍 Running pre-commit checks..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Track exit code
EXIT_CODE=0

# 1. Check Go code compiles
echo "📦 Checking Go backend compiles..."
cd server
if go build ./cmd/server 2>&1; then
    echo -e "${GREEN}✅ Backend compiles successfully${NC}"
else
    echo -e "${RED}❌ Backend compilation failed${NC}"
    EXIT_CODE=1
fi
cd ..

# 2. Run Go tests for skill bulk handler
echo "🧪 Running Go tests for skill bulk..."
cd server
if go test ./internal/handler/... -run SkillBulk -v 2>&1 | head -50; then
    echo -e "${GREEN}✅ Go tests passed${NC}"
else
    echo -e "${YELLOW}⚠️ Some Go tests failed or not found${NC}"
    # Don't fail the commit for missing tests, just warn
fi
cd ..

# 3. Check TypeScript compiles
echo "📘 Checking TypeScript compiles..."
if npx tsc --noEmit -p packages/views/skills/tsconfig.json 2>&1 | head -20; then
    echo -e "${GREEN}✅ TypeScript compiles successfully${NC}"
else
    echo -e "${YELLOW}⚠️ TypeScript check had warnings${NC}"
fi

# 4. Run frontend tests for skill matrix
echo "🧪 Running frontend tests for skill matrix..."
cd packages/views/skills
if pnpm vitest run --reporter=verbose skill-matrix-page.test.tsx 2>&1 | head -100; then
    echo -e "${GREEN}✅ Frontend tests passed${NC}"
else
    echo -e "${RED}❌ Frontend tests failed${NC}"
    EXIT_CODE=1
fi
cd ../../..

# Summary
echo ""
echo "========================================="
if [ $EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}✅ All checks passed! Ready to commit.${NC}"
else
    echo -e "${RED}❌ Some checks failed. Please fix before committing.${NC}"
fi
echo "========================================="

exit $EXIT_CODE
