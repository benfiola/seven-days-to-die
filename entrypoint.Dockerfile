FROM docker.io/golang:1.23 AS entrypoint
WORKDIR /
ADD entrypoint.go entrypoint.go
ADD go.mod go.mod
ADD go.sum go.sum
ADD version.txt version.txt
RUN go build -o /entrypoint entrypoint.go


FROM docker.io/ubuntu:noble

# NOTE: Find manifest id from https://steamdb.info/depot/294422/manifests/
ARG DEPOT_DOWNLOADER_VERSION=2.7.4
ARG MANIFEST_ID

RUN userdel ubuntu && \
    groupadd --gid=1000 server && \
    useradd --gid=server --system --uid=1000 --home /data server && \
    apt -y update && \
    apt -y install curl gosu tar unrar-free vim zip && \
    curl -o archive.zip -fsSL "https://github.com/SteamRE/DepotDownloader/releases/download/DepotDownloader_${DEPOT_DOWNLOADER_VERSION}/DepotDownloader-linux-x64.zip" && \
    mkdir -p extract && \
    unzip archive.zip -d extract && \
    mv extract/DepotDownloader /usr/bin/DepotDownloader && \
    rm -rf archive.zip extract && \
    mkdir -p /server && \
    DepotDownloader -app 294420 -depot 294422 -manifest "${MANIFEST_ID}" -dir /server && \
    chmod +x /server/startserver.sh /server/7DaysToDieServer.x86_64 && \
    mkdir -p /data /generated && \
    chown -R server:server /data /generated /server
COPY --from=entrypoint /entrypoint /entrypoint

EXPOSE 8080/tcp
EXPOSE 8081/tcp
EXPOSE 26900/udp
EXPOSE 26900/tcp
EXPOSE 26901/udp
EXPOSE 26902/udp
EXPOSE 26903/udp
VOLUME /data
WORKDIR /data

ENTRYPOINT ["/entrypoint"]
