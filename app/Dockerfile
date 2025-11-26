FROM golang:1.25 AS builder

WORKDIR /doctray

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -a -o /doctray/docker-doctray


FROM alpine:3.22

RUN mkdir -p /data
WORKDIR /doctray

COPY --from=builder /doctray /doctray

EXPOSE 5555
ENV DOCTRAY_TARGET_DIRECTORY="/data/"

CMD ["/doctray/docker-doctray"]
