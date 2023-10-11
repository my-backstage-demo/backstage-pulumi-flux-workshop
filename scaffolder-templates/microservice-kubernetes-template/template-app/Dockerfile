FROM golang:1.21 as build
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o server main.go

FROM alpine:3.18
WORKDIR /app
COPY --from=build /app/server .
CMD ["./server"]
