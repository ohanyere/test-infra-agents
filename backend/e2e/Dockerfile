FROM gcr.io/distroless/static-debian12:nonroot
COPY bin/ /app/
ENV APP_LATENCY_MS=100 MAX_CONCURRENT_WORK=4
USER nonroot:nonroot
ENTRYPOINT ["/app/referenceapp"]
