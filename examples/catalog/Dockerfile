# The demo server. The build context is the repository root: the example builds the library from
# the working tree (see the replace directive in examples/go.mod). docker-compose.yml sets it up.
FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY examples/go.mod examples/go.sum ./examples/
RUN cd examples && go mod download
COPY . .
RUN cd examples && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./catalog/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
