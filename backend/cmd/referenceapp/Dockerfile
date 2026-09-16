FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/referenceapp ./cmd/referenceapp
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /referenceapp ./cmd/referenceapp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /referenceapp /referenceapp
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/referenceapp"]
