# ===== build stage =====
# this AS builder means we are naming this stage 'builder' so we can reference it later
FROM golang:1.27-alpine AS builder

# setting the working directory inside the container to /src all the following commands will run from here on
WORKDIR /src

# copying the dependency manifests so this layer is cached across all builds & then downloading them
COPY internal/go.mod internal/go.sum ./
RUN go mod download

# copy the go dependency files into the container
COPY internal/. .

# CGO_ENABLED=0 :- disables CGO to create a fully static binary that does not rely on libc (C libraries) 
# GOOS=linux :- ensuring the binary is built specifically to run on Linux system
# flags are to strip away the debugging info and symbols to make the final binary file size even smaller
# and finally saving the compiled binary to a location 
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/raydash .

# ===== run stage =====
# starting a new stage with a minimal alpine linux image
FROM alpine:3.20

# installing ca-certificates & netcat-openbsd packages to make secure HTTPS requests and for health check commands respectively 
RUN apk add --no-cache ca-certificates netcat-openbsd

# copy the compiled binary from our previous specified location of builder stage to this new image root
COPY --from=builder /out/raydash /raydash

# inform docker this container will listen on network port 13203 at runtime
EXPOSE 13203

# health check every 5 second for maximum 5 tries in a 3 second interval i guess before declaring the container as "unhealthy" 
HEALTHCHECK --interval=5s --timeout=3s --retries=5 \
CMD nc -z 127.0.0.1 ${RAYDASH_PORT:-13203} || exit 1

# run our application executable immediately
ENTRYPOINT [ "/raydash" ]