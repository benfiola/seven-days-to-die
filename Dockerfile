FROM docker.io/golang:1.23 AS entrypoint

WORKDIR /app

ADD go.mod go.mod
ADD go.sum go.sum
ADD entrypoint.go entrypoint.go

RUN go build -o /entrypoint entrypoint.go


FROM docker.io/ubuntu:noble

# NOTE: Find manifest id from https://steamdb.info/depot/294422/manifests/
ARG DEPOT_DOWNLOADER_VERSION=2.7.4
ARG MANIFEST_ID

RUN echo "remove ubuntu user" && \
    userdel ubuntu && \
    echo "add sdtd group" && \
    groupadd --gid=1000 sdtd && \
    echo "add sdtd user" && \
    useradd --gid=sdtd --system --uid=1000 --home /data sdtd && \
    echo "install apt dependencies" && \
    apt -y update && \
    apt -y install curl gosu tar unrar-free zip && \
    echo "install DepotDownloader" && \
    curl -o archive.zip -fsSL "https://github.com/SteamRE/DepotDownloader/releases/download/DepotDownloader_${DEPOT_DOWNLOADER_VERSION}/DepotDownloader-linux-x64.zip" && \
    mkdir -p extract && \
    unzip archive.zip -d extract && \
    mv extract/DepotDownloader /usr/bin/DepotDownloader && \
    rm -rf archive.zip extract && \
    echo "download sdtd server: ${MANIFEST_ID}" && \
    mkdir -p /server && \
    DepotDownloader -app 294420 -depot 294422 -manifest "${MANIFEST_ID}" -dir /server && \
    chmod +x /server/startserver.sh /server/7DaysToDieServer.x86_64 && \
    echo "create additional folders" && \
    mkdir -p /data /generated && \
    echo "take folder ownership" && \
    chown -R sdtd:sdtd /data /generated /server
COPY --from=entrypoint /entrypoint /entrypoint

ENV UID=1000
ENV GID="${UID}"
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
