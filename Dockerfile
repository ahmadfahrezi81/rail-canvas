# One image, every binary under /app/. Each service picks one via its start command.

FROM golang:1.27-alpine AS build
WORKDIR /src

COPY api/go.mod api/go.sum ./
RUN go mod download

COPY api/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
EXPOSE 8080
CMD ["/app/api"]
