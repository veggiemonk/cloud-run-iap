FROM golang:1.26 AS build
WORKDIR /src
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1001
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN templ generate && CGO_ENABLED=0 GOOS=linux go build -o /runoauth ./cmd/runoauth

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /runoauth /runoauth
EXPOSE 8080
ENTRYPOINT ["/runoauth"]
