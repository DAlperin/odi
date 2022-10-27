FROM golang:1.19-alpine as build

WORKDIR /app/odi

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o ./out/odi .


FROM docker:20.10.12-alpine3.15

COPY --from=build /app/odi /odi

EXPOSE 8080

CMD ["sh"]