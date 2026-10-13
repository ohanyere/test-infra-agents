FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kubecollector ./cmd/kubecollector

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kubecollector /kubecollector
USER 65532:65532
ENTRYPOINT ["/kubecollector"]
