FROM ubuntu:22.04

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Africa/Lagos

# Install dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    pkg-config \
    libopencv-dev \
    libtesseract-dev \
    libleptonica-dev \
    libpqxx-dev \
    libssl-dev \
    nlohmann-json3-dev \
    postgresql-client \
    tesseract-ocr-eng \
    tzdata \
    && ln -fs /usr/share/zoneinfo/$TZ /etc/localtime \
    && dpkg-reconfigure --frontend noninteractive tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY . .

RUN mkdir build && \
    cd build && \
    cmake .. && \
    make -j"$(nproc)"

EXPOSE 8080

CMD ["./build/ocr_engine"]