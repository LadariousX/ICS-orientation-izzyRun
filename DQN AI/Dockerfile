FROM nvidia/cuda:12.4.1-runtime-ubuntu22.04

# Avoid prompts during installation
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies, Chromium, and Python 3.13
RUN apt-get update && apt-get install -y --no-install-recommends \
    software-properties-common \
    chromium-browser \
    chromium-chromedriver \
    && add-apt-repository ppa:deadsnakes/ppa -y \
    && apt-get update && apt-get install -y --no-install-recommends \
    python3.13-full \
    && rm -rf /var/lib/apt/lists/*

# Set python3.13 as the default 'python' and 'python3' command
RUN ln -sf /usr/bin/python3.13 /usr/bin/python && \
    ln -sf /usr/bin/python3.13 /usr/bin/python3

# CRUCIAL: Bootstrap a fresh pip for Python 3.13 and install setuptools
# This bypasses the broken system pip that relies on distutils.
RUN python -m ensurepip --default-pip && \
    python -m pip install --no-cache-dir --upgrade pip setuptools

WORKDIR /app

# Create the checkpoints directory inside the container
RUN mkdir /checkpoints

# Install Python requirements
COPY requirements.txt .
RUN python -m pip install --no-cache-dir -r requirements.txt

COPY . .

CMD ["python", "DQN AI/train.py", "--url", "http://izzy-game:5003/"]
