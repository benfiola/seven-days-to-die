DEPOT_DOWNLOADER_VERSION := 3.4.0

cwd := $(shell pwd)
temp_dir = $(cwd)/.tmp
depot_downloader_url = https://github.com/SteamRE/DepotDownloader/releases/download/DepotDownloader_${DEPOT_DOWNLOADER_VERSION}/DepotDownloader-linux-x64.zip

.PHONY: default
default:

.PHONY: clean
clean:

.PHONY: clean-entrypoint
clean-entrypoint:
	# remove entrypoint
	rm -rf $(cwd)/entrypoint
clean: clean-entrypoint

.PHONY: build-entrypoint
build-entrypoint:
	# build entrypoint
	go build -o $(cwd)/entrypoint entrypoint.go

.PHONY: clean-depot-downloader
clean:
	# remove depot downloader
	rm -rf $(cwd)/DepotDownloader
clean: clean-depot-downloader

.PHONY: install-depot-downloader
install-depot-downloader:
	# recreate temp dir
	rm -rf $(temp_dir) && mkdir -p $(temp_dir)
	# download depot downloader
	curl -o $(temp_dir)/archive.zip -fsSL $(depot_downloader_url)
	# create extract directory
	mkdir -p $(temp_dir)/extract
	# extract depot downloader
	unzip $(temp_dir)/archive.zip -d $(temp_dir)/extract
	# move binary
	mv $(temp_dir)/extract/DepotDownloader $(cwd)/DepotDownloader
	# remove temp directory directory
	rm -rf $(temp_dir)
