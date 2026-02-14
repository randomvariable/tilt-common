# syntax=docker/dockerfile:1.4
# Custom k3d node image with NVIDIA GPU support
# Based on NVIDIA CUDA runtime image with k3s and nvidia-container-toolkit
#
# Build with BuildKit for efficient caching:
#   DOCKER_BUILDKIT=1 docker build -t k3s-gpu:local .

ARG CUDA_VERSION=12.8.1
ARG UBUNTU_VERSION=24.04
ARG K3S_VERSION=v1.31.6+k3s1

# Stage 1: Download k3s binary with caching
FROM alpine:3.21 AS k3s-download
ARG K3S_VERSION

RUN --mount=type=cache,target=/var/cache/apk,sharing=locked \
    apk add --no-cache curl && \
    curl -fsSL -o /tmp/k3s \
    "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s" && \
    chmod +x /tmp/k3s

# Stage 2: Build final image with BuildKit cache mounts
FROM nvidia/cuda:${CUDA_VERSION}-runtime-ubuntu${UBUNTU_VERSION}

# Install system dependencies with apt cache mount
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        coreutils \
        curl \
        fuse-overlayfs \
        gnupg \
        iptables \
        mount \
        procps

# k3d injects an entrypoint script using #!/bin/sh that relies on bash-style
# trap handling. Ubuntu's default /bin/sh (dash) mishandles signals in the
# wait+trap pattern k3d uses, causing crash loops. Link /bin/sh to bash.
RUN ln -sf /bin/bash /bin/sh

# Install nvidia-container-toolkit with apt cache mount
# Note: We don't use cache for the NVIDIA repo setup since it's small and needs to be in the image
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | \
    gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg && \
    curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
    tee /etc/apt/sources.list.d/nvidia-container-toolkit.list && \
    apt-get update && \
    apt-get install -y nvidia-container-toolkit

# Install k3s binary (renamed to k3s.real since wrapper will be /bin/k3s)
COPY --from=k3s-download /tmp/k3s /bin/k3s.real

# Install k3s wrapper script
COPY k3s-wrapper.sh /bin/k3s
RUN chmod +x /bin/k3s

# Create symlinks for k3s subcommands
RUN ln -s /bin/k3s.real /bin/kubectl \
    && ln -s /bin/k3s.real /bin/ctr \
    && ln -s /bin/k3s.real /bin/crictl

# runc shim for nvidia-container-runtime.
#
# k3s bundles runc inside its binary and extracts it at first boot to
# /var/lib/rancher/k3s/data/current/bin/runc. The nvidia-container-runtime
# delegates to runc but only searches $PATH. This shim forwards the call
# to wherever k3s placed it.
RUN printf '#!/bin/sh\nexec /var/lib/rancher/k3s/data/current/bin/runc "$@"\n' \
        > /usr/bin/runc \
    && chmod 755 /usr/bin/runc

# Configure containerd to use nvidia runtime
COPY nvidia-runtime.toml /etc/containerd/conf.d/nvidia-runtime.toml

# Auto-deploy device plugin when cluster starts
RUN mkdir -p /var/lib/rancher/k3s/server/manifests
COPY device-plugin-daemonset.yaml /var/lib/rancher/k3s/server/manifests/

# Labels for identification
LABEL maintainer="tilt-common"
LABEL description="k3s with NVIDIA GPU support for local development"
LABEL k3s.version="${K3S_VERSION}"
