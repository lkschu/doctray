FROM golang:1.24

RUN mkdir -p /data
WORKDIR /doctray

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -a -o /docker-doctray

EXPOSE 5555
ENV DOCTRAY_TARGET_DIRECTORY="/data/"


CMD ["/docker-doctray"]
