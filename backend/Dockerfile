# Built from the repository root (see docker-compose.yml): the image needs both
# backend/ and ocr-engine/, because the backend runs the C++ OCR engine itself.

# --- OCR engine: the same Ubuntu 22.04 toolchain as ocr-engine/Dockerfile ------
FROM ubuntu:22.04 AS ocr-build

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    cmake \
    pkg-config \
    libopencv-dev \
    libtesseract-dev \
    libleptonica-dev \
    libpqxx-dev \
    libssl-dev \
    nlohmann-json3-dev \
    && rm -rf /var/lib/apt/lists/*

COPY ocr-engine /src/ocr-engine
RUN cmake -S /src/ocr-engine -B /build -DCMAKE_BUILD_TYPE=Release \
    && cmake --build /build --target ocr_engine -j"$(nproc)"

# --- Backend ------------------------------------------------------------------
FROM golang:1.26-alpine AS go-build
WORKDIR /app

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .
# Static binary: the runtime image is glibc-based Ubuntu, not Alpine.
RUN CGO_ENABLED=0 go build -o main .

# --- Runtime --------------------------------------------------------------------
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Exactly the shared libraries ocr_engine links against, plus the English
# Tesseract data it reads, ca-certificates for HTTPS and wget for the health check.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libtesseract4 \
    tesseract-ocr-eng \
    libopencv-core4.5d \
    libopencv-imgproc4.5d \
    libopencv-imgcodecs4.5d \
    libopencv-photo4.5d \
    libpqxx-6.4 \
    libssl3 \
    ca-certificates \
    wget \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ocr-build /build/ocr_engine /usr/local/bin/ocr_engine

# Fail the build, not the first receipt, if a runtime library is missing.
RUN if ldd /usr/local/bin/ocr_engine | grep "not found"; then \
        echo "ocr_engine has unresolved shared libraries" >&2; exit 1; \
    fi

# The backend refuses to start without this binary. LOG_FILE keeps the engine's
# own log out of the filesystem; its diagnostics still reach the backend on stderr.
ENV OCR_ENGINE_PATH=/usr/local/bin/ocr_engine \
    LOG_FILE=/dev/null

WORKDIR /app

COPY --from=go-build /app/main ./main
COPY --from=go-build /app/frontend ./frontend

EXPOSE 8080

CMD ["./main"]
