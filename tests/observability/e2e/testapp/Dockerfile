# Copyright 2026 Naadir Jeewa
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /testapp .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /testapp /testapp
ENTRYPOINT ["/testapp"]
