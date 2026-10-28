# Сборка вынесена в отдельный этап: инструменты Go и исходники нужны только
# для компиляции, а в готовый образ попадает один статический файл.
FROM golang:1.26-alpine AS source

WORKDIR /src

# Зависимости копируются раньше исходников, чтобы их загрузка переиспользовалась
# между сборками, пока go.mod и go.sum не менялись.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

FROM source AS test
CMD ["sh", "scripts/container-tests.sh"]

FROM source AS build

# CGO отключён, иначе получится динамический файл, которому в образе без libc
# не на что опереться. Отладочные таблицы и пути сборки выброшены.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/devseed ./cmd/devseed


FROM alpine:3.21 AS runtime

# Сертификаты нужны для обращений к внешним источникам по HTTPS.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 app

USER app

FROM runtime AS devseed
COPY --from=build /out/devseed /usr/local/bin/devseed
ENTRYPOINT ["devseed"]
CMD ["-days=3"]

# Последний этап остаётся сервером: существующий docker build и CI не меняются.
FROM runtime AS api
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080

# Миграции сервер накатывает сам при запуске, поэтому отдельного шага
# подготовки базы в оркестрации не требуется. Проверка готовности опрашивает
# тот же эндпоинт, который сообщает и о доступности базы.
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget -q -O - http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["api"]
