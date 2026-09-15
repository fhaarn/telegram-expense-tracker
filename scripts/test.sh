#!/usr/bin/env bash
# Run integration-tagged Go tests (including ordinary unit tests).
# Never read .env or use DATABASE_URL: test data belongs in a separate database.
set -euo pipefail

test_container=''
cleanup() {
  status=$?
  trap - EXIT
  if [[ -n "$test_container" ]]; then
    if ! docker rm -f "$test_container" >/dev/null 2>&1; then
      echo "Could not remove temporary test container: $test_container" >&2
    fi
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
  if ! command -v docker >/dev/null || ! docker info >/dev/null 2>&1; then
    echo 'Start Docker, or supply TEST_DATABASE_URL pointing to a dedicated test database.' >&2
    exit 1
  fi
  test_container="expense-tracker-test-$$-$RANDOM"
  echo 'Starting temporary PostgreSQL for tests…'
  docker run --rm -d --name "$test_container" \
    -e POSTGRES_USER=expense -e POSTGRES_PASSWORD=expense \
    -e POSTGRES_DB=expense_tracker_test \
    -p 127.0.0.1::5432 postgres:17-alpine >/dev/null

  ready=false
  for ((attempt=0; attempt<60; attempt++)); do
    if docker exec "$test_container" pg_isready -h 127.0.0.1 -U expense -d expense_tracker_test >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done
  if [[ "$ready" != true ]]; then
    echo 'Temporary PostgreSQL did not become ready within 60 seconds.' >&2
    exit 1
  fi
  address=$(docker port "$test_container" 5432/tcp)
  export TEST_DATABASE_URL="postgres://expense:expense@$address/expense_tracker_test?sslmode=disable"
else
  echo 'Using the supplied TEST_DATABASE_URL (value hidden).'
fi

if [[ $# -eq 0 ]]; then set -- ./...; fi
go test -race -count=1 -timeout=2m -tags=integration "$@"
