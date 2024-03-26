FROM golang:1.22

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY *.go ./

RUN mkdir /app/music
COPY ./music/*.aac /app/music

RUN GOOS=linux go build -v -o /audio-streamer

EXPOSE 8080

CMD ["/audio-streamer"]

