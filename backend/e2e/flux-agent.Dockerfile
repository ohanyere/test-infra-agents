FROM gcr.io/distroless/static-debian12:nonroot
COPY bin/fluxagent /app/fluxagent
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/app/fluxagent"]
