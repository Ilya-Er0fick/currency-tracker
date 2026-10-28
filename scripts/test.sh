#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

if docker compose version >/dev/null 2>&1; then
    compose=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    compose=(docker-compose)
else
    echo 'Нужен Docker Compose v2 или новее.' >&2
    exit 1
fi

# Уникальный проект отделяет прогон от разработки и от параллельных прогонов.
project="currency-tracker-test-$(date +%s)-$$"
compose+=(--project-name "$project" -f compose.test.yaml --profile test)
cleanup() {
    local status=$?
    trap - EXIT
    if ! "${compose[@]}" down --volumes; then
        echo "Не удалось удалить тестовые контейнеры проекта $project." >&2
        if [ "$status" -eq 0 ]; then status=1; fi
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"${compose[@]}" up --build --abort-on-container-exit --exit-code-from server-tests server-tests
