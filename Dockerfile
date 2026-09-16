FROM alpine:3.22 AS builder
ARG TARGETPLATFORM
RUN echo "I'm building for $TARGETPLATFORM"

RUN apk add --no-cache gzip && \
    mkdir /mihomo-config && \
    wget -O /mihomo-config/geoip.metadb https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.metadb && \
    wget -O /mihomo-config/geosite.dat https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat && \
    wget -O /mihomo-config/geoip.dat https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat

COPY docker/file-name.sh /mihomo/file-name.sh
WORKDIR /mihomo
COPY bin/ bin/
RUN FILE_NAME=`sh file-name.sh` && echo $FILE_NAME && \
    FILE_NAME=`ls bin/ | egrep "$FILE_NAME.gz"|awk NR==1` && echo $FILE_NAME && \
    mv bin/$FILE_NAME mihomo.gz && gzip -d mihomo.gz && chmod +x mihomo && echo "$FILE_NAME" > /mihomo-config/test
FROM alpine:3.22
LABEL org.opencontainers.image.source="https://github.com/MetaCubeX/mihomo"

RUN apk add --no-cache ca-certificates tzdata iptables

# Kept running as root on purpose:
# - The baked-in geo databases and the default VOLUME live in /root/.config/mihomo;
#   alpine's /root is mode 700, so an unprivileged user cannot reach them, and
#   relocating them would break existing volume mounts.
# - TUN mode needs NET_ADMIN on /dev/net/tun; --cap-add only takes effect for the
#   root process (non-root would need ambient capabilities).
# To run non-root, override explicitly, e.g.:
#   docker run --user mihomo -e HOME=/home/mihomo ... (config then at $HOME/.config/mihomo)

VOLUME ["/root/.config/mihomo/"]

COPY --from=builder /mihomo-config/ /root/.config/mihomo/
COPY --from=builder /mihomo/mihomo /mihomo
ENTRYPOINT [ "/mihomo" ]
