FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gadget-store .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/gadget-store ./gadget-store
COPY templates ./templates
# Preserve bundled product photos on the initial deployment. New production
# uploads are stored in Supabase Storage because the container filesystem is ephemeral.
COPY uploads ./uploads
RUN chown -R app:app /app
USER app
EXPOSE 10000
CMD ["./gadget-store"]
