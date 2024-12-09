FROM docker.io/ubuntu:noble AS server
ARG DEPOT_DOWNLOADER_VERSION=2.7.4
# NOTE: Find manifest id from https://steamdb.info/depot/294422/manifests/
ARG MANIFEST_ID=2366383150472453701
WORKDIR /server
RUN apt -y update && \
    apt -y install curl zip && \
    curl -o archive.zip -fsSL "https://github.com/SteamRE/DepotDownloader/releases/download/DepotDownloader_${DEPOT_DOWNLOADER_VERSION}/DepotDownloader-linux-x64.zip" && \
    mkdir -p extract && \
    unzip archive.zip -d extract && \
    mv extract/DepotDownloader /usr/bin/DepotDownloader && \
    rm -rf archive.zip extract && \
    DepotDownloader -app 294420 -depot 294422 -manifest "${MANIFEST_ID}" -dir . && \
    chmod +x startserver.sh 7DaysToDieServer.x86_64

FROM docker.io/golang:1.23 AS helper
WORKDIR /app
ADD go.mod go.mod
ADD go.sum go.sum
ADD main.go main.go
RUN go build -o /helper main.go

FROM docker.io/ubuntu:noble AS final
EXPOSE 26900/udp 
EXPOSE 26900/tcp
EXPOSE 26901/udp
EXPOSE 26902/udp

COPY from=helper /helper /helper
COPY from=server /server /server
