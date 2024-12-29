# seven-days-to-die

This repo provides the necessary code to create a [7 Days to Die](https://7daystodie.com/) dedicated server docker image.

While other 7 Days to Die dedicated server docker images exist and certainly work, this image is simplified. It:

- Provides docker images tagged to [specific builds](https://steamdb.info/depot/294422/manifests/) of the dedicated server. These builds are part of the container, not downloaded on launch.
- Simplifies mod and 'root file' (i.e., data installed to server root folder) installation
- Generates server settings from environment variables
- Does not provide update mechanisms for games, mods or otherwise
- Does not provide built-in health-check monitoring
- Does not provide built-in alerting
- Does not provide built-in backup mechanisms
- Logs to stdout/stderr

It's assumed that health checks, backups and updates are managed externally via infrastructure-as-code and container orchestration.

## Usage

This docker image is hosted on the Docker hub. You can pull this image at `docker.io/benfiola/seven-days-to-die:[manifest-id]`. Because this image is designed to be pinned at specific game versions, there are no _latest_ images. Use [SteamDB](https://steamdb.info/depot/294422/manifests/) to find the current manifest ID.

Here are some basic deployment examples:

- [docker-compose](./examples/docker-compose.yaml)
- [kubernetes](./examples/kubernetes.yaml)

Additionally, [here](./examples/default-serverconfig.xml) is the default `serverconfig.xml` that ships with the game (as of manifest `6852366042385286885`).

## Configuration

The docker image is configured purely through the environment:

| Variable            | Default | Description                                                                                 |
| ------------------- | ------- | ------------------------------------------------------------------------------------------- |
| DELETE_DEFAULT_MODS | 0       | Delete the default mods that come with the game. Some overhaul mods require this.           |
| GID                 | 1000    | The GID to run the server as                                                                |
| MOD_URLS            |         | A comma-separated list of URLs to be downloaded and extracted to the `[server]/Mods` folder |
| ROOT_URLS           |         | A comma-separated list of URLs to be downloaded and extracted to the `[server]` folder.     |
| SETTING\_[Key]      |         | Defines a property named `[Key]` in the `serverconfig.xml` file                             |
| UID                 | 1000    | The UID to run the server as                                                                |

## Server Data

The docker image is configured to host server data in the `/data` folder. For persistence, you will need to mount a local path (or, _PersistentVolume_ if Kubernetes, to the `/data` folder).

## UID/GID

The docker image is configured to run under a non-root user.

If a container is run as the root user, the entrypoint will attempt to take ownership of necessary files under the values of environment variables `UID` and `GID` and relaunch itself.

If a container is run as a non-root user, the entrypoint will run as this non-root user. It's assumed that necessary files are already owned by the current non-root user.

## Health check

You can perform a health check on a running server by running the `/entrypoint health` command. This is useful for configuring things like Kubernetes liveness/readiness probes.

## Entrypoint

The entrypoint is implemented in golang and is defined at [./cmd/entrypoint/main.go](./cmd/entrypoint/main.go). It's (hopefully) well-documented - feel free to take a look!

## Development

This project was written using [VSCode](https://code.visualstudio.com/) and the [devcontainers](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) extension. Use these for a streamlined development experience.

PRs and feedback are welcome.

## TODO

- Automate creating new builds
