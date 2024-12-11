# seven-days-to-die

This repo provides the necessary code to create a [7 Days to Die](https://7daystodie.com/) dedicated server docker image.

While other 7 Days to Die dedicated server docker images exist and certainly work, this image is simplified. It:

- Provides docker images tagged to [specific builds](https://steamdb.info/depot/294422/manifests/) of the dedicated server
- Simplifies mod and 'root file' (i.e., data installed to server root folder) installation
- Does not provide update mechanisms for games, mods or otherwise
- Does not provide built-in health-check mechanisms
- Does not provide built-in backup mechanisms

It's assumed that health checks, backups and updates are managed externally via infrastructure-as-code and container orchestration.

## Usage

This docker image is hosted on the Docker hub. You can pull this image at `docker.io/benfiola/seven-days-to-die:[manifest-id]`. Because this image is designed to be pinned at specific game versions, there are no _latest_ images. Use [SteamDB](https://steamdb.info/depot/294422/manifests/) to find the current manifest ID.

## Configuration

The docker image is configured purely through the environment:

| Variable       | Default | Description                                                                                 |
| -------------- | ------- | ------------------------------------------------------------------------------------------- |
| GID            | 1000    | The GID to run the server as                                                                |
| MOD_URLS       |         | A comma-separated list of URLs to be downloaded and extracted to the `[server]/Mods` folder |
| ROOT_URLS      |         | A comma-separated list of URLs to be downloaded and extracted to the `[server]` folder      |
| SETTING\_[Key] |         | Defines a property named `[Key]` in the `<server>/serverconfig.xml` file                    |
| UID            | 1000    | The UID to run the server as                                                                |

## Server Data

The docker image is configured to host server data in the `/data` folder. For persistence, you will need to mount a local path (or, _PersistentVolume_ if Kubernetes, to the `/data` folder).

## Entrypoint

The entrypoint is implemented in golang and is defined at [./entrypoint.go](./entrypoint.go). It's (hopefully) well-documented - feel free to take a look!

## Development

This project was written using [VSCode](https://code.visualstudio.com/) and the [devcontainers](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) extension. Use these for a streamlined development experience.

PRs and feedback are welcome.
