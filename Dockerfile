FROM docker.io/golang:1.23 AS entrypoint
WORKDIR /
ADD entrypoint.go entrypoint.go 
ADD go.mod go.mod
ADD go.sum go.sum
ADD Makefile Makefile
ADD version.txt version.txt
RUN <<EOF
make build-entrypoint
EOF

FROM docker.io/ubuntu:noble AS depot-downloader
WORKDIR /
ADD Makefile Makefile
RUN <<EOF
apt -y update
apt -y install curl make unzip
make install-depot-downloader
EOF

FROM docker.io/ubuntu:noble
WORKDIR /
RUN <<EOF
# install dependencies
apt -y update
apt -y install curl gosu squashfs-tools tar unrar-free unzip
userdel ubuntu
# create user
groupadd --gid=1000 server
useradd --gid=server --system --uid=1000 --create-home server
# create container paths
mkdir -p /cache /data /generated /sdtd
chown -R server:server /cache /data /generated /sdtd
EOF
COPY --from=entrypoint /entrypoint /usr/local/bin/entrypoint
COPY --from=depot-downloader /DepotDownloader /usr/local/bin/DepotDownloader
ENTRYPOINT ["entrypoint"]
EXPOSE 8080/tcp
EXPOSE 8081/tcp
EXPOSE 26900/udp
EXPOSE 26900/tcp
EXPOSE 26901/udp
EXPOSE 26902/udp
EXPOSE 26903/udp
VOLUME /cache
VOLUME /data
