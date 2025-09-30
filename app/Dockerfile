FROM golang:1.24

WORKDIR /doctray

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -a -o /docker-doctray

EXPOSE 5555
ENV DOCTRAY_TARGET_DIRECTORY="/data/"

RUN mkdir -p /data

CMD ["/docker-doctray"]
