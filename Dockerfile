FROM heroiclabs/nakama-pluginbuilder:3.27.0 AS builder
ENV GO111MODULE=on CGO_ENABLED=1
WORKDIR /backend
COPY go.mod go.sum ./
RUN go mod download
COPY *.go arena.json ./
RUN go build -trimpath -buildmode=plugin -o /backend/arena.so .

FROM heroiclabs/nakama:3.27.0
COPY --from=builder /backend/arena.so /nakama/data/modules/arena.so
