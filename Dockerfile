# Minimal FIPS-compliant runtime image.
# The binary must have been built with Dockerfile.fips-builder beforehand.
# At runtime the binary dlopens gardenlinux's FIPS 140-validated OpenSSL,
# which is pre-configured as the default crypto provider in this image.
FROM ghcr.io/gardenlinux/gardenlinux/fips:1877.23
ARG TARGETOS
ARG TARGETARCH
ARG COMPONENT
ARG KIND_VERSION=v0.33.0
WORKDIR /
COPY bin/$COMPONENT.$TARGETOS-fips-$TARGETARCH /<component>
RUN apt-get update && apt-get install -y --no-install-recommends docker-cli curl ca-certificates \
    && curl -fsSL "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${TARGETARCH}" -o /usr/local/bin/kind \
    && chmod +x /usr/local/bin/kind \
    && apt-get purge -y curl \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*
# USER 65532:65532

# docker doesn't substitue args in ENTRYPOINT, so we replace this during the build script
ENTRYPOINT ["/<component>"]
