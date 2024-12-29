#!/bin/bash
set -e

export DEBIAN_FRONTEND=noninteractive

# install add-apt-repository
apt -y update
apt -y install software-properties-common

# install steamcmd
add-apt-repository -y multiverse
dpkg --add-architecture i386
apt -y update
echo steam steam/question select "I AGREE" | debconf-set-selections
echo steam steam/license note '' | debconf-set-selections
apt -y install steamcmd
ln -s /usr/games/steamcmd /usr/local/bin/steamcmd

unset DEBIAN_FRONTEND