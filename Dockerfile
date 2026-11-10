# SPDX-License-Identifier: Apache-2.0
#
# The Sentinel server image (§11.1). It holds the release's own signed,
# reproducibly built binary (from scripts/release.sh's dist/), not one
# built again here, so the image runs exactly what was signed.
#
#   scripts/release.sh all v1.2.3 linux/amd64 linux/arm64
#   docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=v1.2.3 -t sentinel-server .
#
# The base is distroless/static: no shell or package manager, a nonroot
# user, CA certificates (for ACME and fetching releases) and tzdata.
# The data directory must exist in the image, owned by the nonroot user, so
# a new volume mounted on it is writable; distroless has no shell to make it.
FROM gcr.io/distroless/static-debian12:debug-nonroot AS folders
RUN ["/busybox/mkdir", "-p", "/tmp/out/var/lib/sentinel"]

FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION
ARG TARGETARCH
COPY --from=folders --chown=65532:65532 --chmod=0700 /tmp/out/var/lib/sentinel /var/lib/sentinel
COPY --chown=0:0 --chmod=0755 dist/sentinel-server_${VERSION}_linux_${TARGETARCH} /usr/local/bin/sentinel-server

LABEL org.opencontainers.image.title="Sentinel server" \
      org.opencontainers.image.description="Self-hosted remote monitoring and management" \
      org.opencontainers.image.source="https://github.com/aazam476/sentinel" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"

# The database, keys and certificates. Mount a volume here.
VOLUME /var/lib/sentinel
EXPOSE 7443 3478/udp 49152-49251/udp
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/sentinel-server", "--listen", ":7443", "--data-dir", "/var/lib/sentinel"]
