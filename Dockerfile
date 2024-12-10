FROM docker.io/golang:1.23 AS entrypoint

WORKDIR /app

ADD go.mod go.mod
ADD go.sum go.sum
ADD main.go main.go

RUN go build -o /entrypoint main.go


FROM docker.io/ubuntu:noble

# NOTE: Find manifest id from https://steamdb.info/depot/294422/manifests/
ARG DEPOT_DOWNLOADER_VERSION=2.7.4
ARG MANIFEST_ID

ENV UID=1000
ENV GID="${UID}"

WORKDIR /server

RUN apt -y update && \
    apt -y install curl gosu tar unrar-free zip && \
    curl -o archive.zip -fsSL "https://github.com/SteamRE/DepotDownloader/releases/download/DepotDownloader_${DEPOT_DOWNLOADER_VERSION}/DepotDownloader-linux-x64.zip" && \
    mkdir -p extract && \
    unzip archive.zip -d extract && \
    mv extract/DepotDownloader /usr/bin/DepotDownloader && \
    rm -rf archive.zip extract && \
    DepotDownloader -app 294420 -depot 294422 -manifest "${MANIFEST_ID}" -dir . && \
    chmod +x startserver.sh 7DaysToDieServer.x86_64

COPY --from=entrypoint /entrypoint /entrypoint

ENTRYPOINT ["/entrypoint"]
