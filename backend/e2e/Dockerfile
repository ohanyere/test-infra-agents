FROM gcr.io/distroless/static-debian12:nonroot
COPY bin/ /app/
COPY git-fixture/ /git-fixture/
ARG APP_ERROR_RATE=0
ENV APP_LATENCY_MS=100 MAX_CONCURRENT_WORK=4 APP_ERROR_RATE=${APP_ERROR_RATE}
USER nonroot:nonroot
ENTRYPOINT ["/app/referenceapp"]
