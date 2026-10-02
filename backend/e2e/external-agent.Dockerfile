FROM gcr.io/distroless/static-debian12:nonroot
COPY bin/externalagent /app/externalagent
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/app/externalagent"]
