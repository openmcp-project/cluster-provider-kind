FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
ARG TARGETOS
ARG TARGETARCH
ARG COMPONENT
WORKDIR /
COPY bin/$COMPONENT.$TARGETOS-$TARGETARCH /<component>
RUN apk add --no-cache docker-cli kind
# USER 65532:65532

# docker doesn't substitue args in ENTRYPOINT, so we replace this during the build script
ENTRYPOINT ["/<component>"]
