FROM ubuntu:22.04

# Install dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    pkg-config \
    libopencv-dev \
    libtesseract-dev \
    libleptonica-dev \
    libpqxx-dev \
    postgresql-client \
    tesseract-ocr-eng \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY . .

RUN mkdir build && cd build && \
    cmake .. && \
    make -j$(nproc)

EXPOSE 8080

CMD ["./build/ocr_engine"]
